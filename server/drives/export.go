package drives

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
)

// exportJSON streams the contents of db out to w as a drive-data.json
// payload matching the StoreData shape that Sentry Studio reads from the
// archive copy.
//
// Streaming properties (load-bearing on a 512 MB Pi Zero 2 W with a
// ~500 MB DB):
//   - One route row is fetched, its BLOB columns decoded, and the
//     resulting Route struct marshaled to JSON bytes into w — then the
//     next row. Peak Go-side allocation is ~1 Route (~100-200 KB).
//   - processed_files is streamed as an array of strings (one per row).
//   - drive_tags is materialized into a map[string][]string first
//     because we need to group tags under each drive_key for the JSON
//     shape. Tags are tiny so this map is small even on huge datasets.
//
// Deterministic output: routes are emitted in file order, processed_files
// in lexical order, so the exported JSON is stable across runs (useful
// for diffing against archive snapshots).
//
// The top-level JSON shape is:
//
//	{
//	  "processedFiles": [ ... ],
//	  "routes":         [ ... ],
//	  "driveTags":      { ... }   // omitted if empty
//	}
//
// This matches what json.Marshal(StoreData{...}) would produce (with
// DriveTags omitempty), so Sentry Studio continues to read the archive
// copy unchanged.
func exportJSON(ctx context.Context, db *sql.DB, w io.Writer) error {
	// Use a buffered writer? Caller decides. The sqlite driver hands us
	// bytes a row at a time, encoding/json.Encoder already buffers per
	// Encode call, and every archive writer we care about (os.File,
	// http.ResponseWriter) is already buffered above us.

	bw := &trackedWriter{w: w}

	if _, err := bw.Write([]byte("{")); err != nil {
		return err
	}

	if err := writeProcessedFilesArray(ctx, db, bw); err != nil {
		return fmt.Errorf("exportJSON: processedFiles: %w", err)
	}
	if _, err := bw.Write([]byte(",")); err != nil {
		return err
	}
	if err := writeRoutesArray(ctx, db, bw); err != nil {
		return fmt.Errorf("exportJSON: routes: %w", err)
	}

	hasTags, err := writeDriveTagsObject(ctx, db, bw)
	if err != nil {
		return fmt.Errorf("exportJSON: driveTags: %w", err)
	}
	_ = hasTags // already written with its own leading comma if present

	if _, err := bw.Write([]byte("}")); err != nil {
		return err
	}
	return nil
}

// trackedWriter is a tiny io.Writer wrapper we use to smuggle some state
// around (future: track bytes written for progress). For now it's just a
// pass-through; keeping it as a struct makes the next feature a two-line
// change.
type trackedWriter struct {
	w io.Writer
	n int64
}

func (t *trackedWriter) Write(p []byte) (int, error) {
	n, err := t.w.Write(p)
	t.n += int64(n)
	return n, err
}

// writeProcessedFilesArray emits `"processedFiles":[...]` streaming one
// string per row so memory stays bounded regardless of row count.
func writeProcessedFilesArray(ctx context.Context, db *sql.DB, w io.Writer) error {
	if _, err := io.WriteString(w, `"processedFiles":[`); err != nil {
		return err
	}
	rows, err := db.QueryContext(ctx, `SELECT file FROM processed_files ORDER BY file`)
	if err != nil {
		return err
	}
	defer rows.Close()

	enc := json.NewEncoder(w)
	first := true
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return err
		}
		if !first {
			if _, err := io.WriteString(w, ","); err != nil {
				return err
			}
		}
		first = false
		// json.Encoder adds a trailing newline; we trim it implicitly by
		// using Marshal directly below. Actually simpler: use
		// json.Marshal to get the exact string bytes.
		b, err := json.Marshal(f)
		if err != nil {
			return err
		}
		if _, err := w.Write(b); err != nil {
			return err
		}
		_ = enc // reserved for future use
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := io.WriteString(w, `]`); err != nil {
		return err
	}
	return nil
}

// writeRoutesArray emits `"routes":[...]` streaming one route object per
// row. Each row's BLOB columns are decoded into a Route struct and
// marshaled to JSON — the whole Route is the unit of memory.
func writeRoutesArray(ctx context.Context, db *sql.DB, w io.Writer) error {
	if _, err := io.WriteString(w, `"routes":[`); err != nil {
		return err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT file, date_dir, raw_park_count, raw_frame_count,
		       points_blob, gear_states_blob, ap_states_blob,
		       speeds_blob, accel_blob, gear_runs_blob
		FROM routes
		ORDER BY file`)
	if err != nil {
		return err
	}
	defer rows.Close()

	first := true
	for rows.Next() {
		var r Route
		var pb, gb, ab, sb, acb, rb []byte
		if err := rows.Scan(
			&r.File, &r.Date, &r.RawParkCount, &r.RawFrameCount,
			&pb, &gb, &ab, &sb, &acb, &rb,
		); err != nil {
			return err
		}
		points, err := decodePoints(pb)
		if err != nil {
			return fmt.Errorf("route %q: %w", r.File, err)
		}
		r.Points = points
		r.GearStates = decodeUint8s(gb)
		r.AutopilotStates = decodeUint8s(ab)
		speeds, err := decodeFloat32s(sb)
		if err != nil {
			return fmt.Errorf("route %q: %w", r.File, err)
		}
		r.Speeds = speeds
		accel, err := decodeFloat32s(acb)
		if err != nil {
			return fmt.Errorf("route %q: %w", r.File, err)
		}
		r.AccelPositions = accel
		runs, err := decodeGearRuns(rb)
		if err != nil {
			return fmt.Errorf("route %q: %w", r.File, err)
		}
		r.GearRuns = runs

		if !first {
			if _, err := io.WriteString(w, ","); err != nil {
				return err
			}
		}
		first = false

		b, err := json.Marshal(&r)
		if err != nil {
			return fmt.Errorf("route %q: marshal: %w", r.File, err)
		}
		if _, err := w.Write(b); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := io.WriteString(w, `]`); err != nil {
		return err
	}
	return nil
}

// writeDriveTagsObject emits `,"driveTags":{...}` if there are any tags;
// returns hasTags=true if it wrote anything. Caller doesn't need to add
// a leading comma — this function handles that internally.
func writeDriveTagsObject(ctx context.Context, db *sql.DB, w io.Writer) (bool, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT drive_key, tag FROM drive_tags ORDER BY drive_key, tag`)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	byKey := map[string][]string{}
	var keyOrder []string
	for rows.Next() {
		var key, tag string
		if err := rows.Scan(&key, &tag); err != nil {
			return false, err
		}
		if _, seen := byKey[key]; !seen {
			keyOrder = append(keyOrder, key)
		}
		byKey[key] = append(byKey[key], tag)
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if len(byKey) == 0 {
		return false, nil // omitempty
	}

	if _, err := io.WriteString(w, `,"driveTags":{`); err != nil {
		return true, err
	}
	for i, key := range keyOrder {
		if i > 0 {
			if _, err := io.WriteString(w, ","); err != nil {
				return true, err
			}
		}
		kb, err := json.Marshal(key)
		if err != nil {
			return true, err
		}
		if _, err := w.Write(kb); err != nil {
			return true, err
		}
		if _, err := io.WriteString(w, ":"); err != nil {
			return true, err
		}
		vb, err := json.Marshal(byKey[key])
		if err != nil {
			return true, err
		}
		if _, err := w.Write(vb); err != nil {
			return true, err
		}
	}
	if _, err := io.WriteString(w, `}`); err != nil {
		return true, err
	}
	return true, nil
}
