package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/creack/pty"
	"golang.org/x/net/websocket"
)

// terminalRateLimit tracks failed auth attempts per remote IP to prevent brute
// force attacks on the PTY endpoint.
var terminalRateLimit = struct {
	mu       sync.Mutex
	failures map[string][]time.Time // IP → timestamps of recent failures
}{
	failures: make(map[string][]time.Time),
}

const (
	terminalRateWindow   = 5 * time.Minute // sliding window
	terminalRateMaxFails = 5               // max failures per window
)

// terminalRateLimited returns true if the given IP has exceeded the failure
// threshold within the sliding window.
func terminalRateLimited(ip string) bool {
	terminalRateLimit.mu.Lock()
	defer terminalRateLimit.mu.Unlock()

	cutoff := time.Now().Add(-terminalRateWindow)
	times := terminalRateLimit.failures[ip]

	// Prune expired entries
	valid := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	if len(valid) == 0 {
		delete(terminalRateLimit.failures, ip)
	} else {
		terminalRateLimit.failures[ip] = valid
	}

	return len(valid) >= terminalRateMaxFails
}

// terminalRecordFailure records a failed auth attempt for the given IP.
func terminalRecordFailure(ip string) {
	terminalRateLimit.mu.Lock()
	defer terminalRateLimit.mu.Unlock()
	terminalRateLimit.failures[ip] = append(terminalRateLimit.failures[ip], time.Now())
}

type terminalAuth struct {
	Type     string `json:"type"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type terminalResize struct {
	Type string `json:"type"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

type terminalMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
}

// handleTerminal upgrades to WebSocket and provides a PTY-backed terminal.
// Flow:
//  1. Client sends {"type":"auth","username":"...","password":"..."} as first message
//  2. Server validates credentials via `su -c whoami`
//  3. On success, spawns `su -l <user>` with a PTY and bridges I/O over the WebSocket
//  4. Client sends {"type":"input","data":"..."} for keystrokes
//  5. Client sends {"type":"resize","cols":N,"rows":N} for terminal resize
func (h *handlers) handleTerminal(w http.ResponseWriter, r *http.Request) {
	s := websocket.Server{
		Handler: func(conn *websocket.Conn) {
			defer conn.Close()

			remoteIP := conn.Request().RemoteAddr
			// Strip port from remote address
			if idx := strings.LastIndex(remoteIP, ":"); idx != -1 {
				remoteIP = remoteIP[:idx]
			}

			// Check rate limit before processing auth
			if terminalRateLimited(remoteIP) {
				log.Printf("[terminal] Rate limited auth attempt from %s", remoteIP)
				sendTermMsg(conn, "error", "Too many failed attempts. Try again later.")
				return
			}

			// Step 1: Wait for auth message
			var authMsg terminalAuth
			if err := websocket.JSON.Receive(conn, &authMsg); err != nil {
				sendTermMsg(conn, "error", "Failed to read auth message")
				return
			}
			if authMsg.Type != "auth" || authMsg.Username == "" || authMsg.Password == "" {
				sendTermMsg(conn, "error", "Invalid auth message")
				return
			}

			// Step 2: Validate credentials using su
			if !validateCredentials(authMsg.Username, authMsg.Password) {
				terminalRecordFailure(remoteIP)
				sendTermMsg(conn, "auth_failed", "Invalid username or password")
				return
			}

			sendTermMsg(conn, "auth_ok", "")

			// Step 3: Spawn PTY with su -l <username>
			cmd := exec.Command("su", "-l", authMsg.Username)
			cmd.Env = append(os.Environ(),
				"TERM=xterm-256color",
				fmt.Sprintf("HOME=/home/%s", authMsg.Username),
			)

			ptmx, err := pty.Start(cmd)
			if err != nil {
				sendTermMsg(conn, "error", "Failed to start terminal: "+err.Error())
				return
			}
			defer func() {
				ptmx.Close()
				if cmd.Process != nil {
					cmd.Process.Signal(syscall.SIGHUP)
					cmd.Process.Wait()
				}
			}()

			var wg sync.WaitGroup

			// PTY → WebSocket (stdout)
			wg.Add(1)
			go func() {
				defer wg.Done()
				buf := make([]byte, 4096)
				for {
					n, err := ptmx.Read(buf)
					if err != nil {
						if err != io.EOF {
							log.Printf("terminal: PTY read error: %v", err)
						}
						return
					}
					msg := terminalMessage{Type: "output", Data: string(buf[:n])}
					if err := websocket.JSON.Send(conn, msg); err != nil {
						return
					}
				}
			}()

			// WebSocket → PTY (stdin + resize)
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					var raw json.RawMessage
					if err := websocket.JSON.Receive(conn, &raw); err != nil {
						return
					}

					var base struct {
						Type string `json:"type"`
					}
					if err := json.Unmarshal(raw, &base); err != nil {
						continue
					}

					switch base.Type {
					case "input":
						var msg terminalMessage
						if err := json.Unmarshal(raw, &msg); err != nil {
							continue
						}
						if _, err := ptmx.Write([]byte(msg.Data)); err != nil {
							return
						}

					case "resize":
						var msg terminalResize
						if err := json.Unmarshal(raw, &msg); err != nil {
							continue
						}
						setTermSize(ptmx, msg.Rows, msg.Cols)

					case "ping":
						sendTermMsg(conn, "pong", "")
					}
				}
			}()

			// Wait for process to exit
			cmd.Wait()
			sendTermMsg(conn, "exit", "Terminal session ended")
			wg.Wait()
		},
	}
	s.ServeHTTP(w, r)
}

// verifyPasswordScript is a Perl script that reads the password from stdin
// and verifies it against /etc/shadow. Perl's crypt() calls the system's
// crypt(3) which supports all hash formats (SHA-512, yescrypt, etc.).
// Username is passed as the first argument.
const verifyPasswordScript = `use strict;
use warnings;
my $username = $ARGV[0];
my $password = <STDIN>;
chomp $password;
open(my $fh, '<', '/etc/shadow') or exit 1;
while (<$fh>) {
    chomp;
    my @parts = split(/:/, $_, -1);
    if ($parts[0] eq $username) {
        my $stored = $parts[1];
        exit 1 if !$stored || $stored eq '*' || $stored eq '!!' || $stored =~ /^!/;
        exit(crypt($password, $stored) eq $stored ? 0 : 1);
    }
}
exit 1;`

// validateCredentials checks if the username/password is valid by reading
// /etc/shadow and verifying the password hash via Perl's crypt(). The server
// runs as root so it has direct access to the shadow file.
func validateCredentials(username, password string) bool {
	// Check that the user exists on the system
	if _, err := user.Lookup(username); err != nil {
		log.Printf("[terminal] validateCredentials: user %q not found: %v", username, err)
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Password is passed via stdin to avoid exposing it in /proc/PID/cmdline.
	cmd := exec.CommandContext(ctx, "perl", "-e", verifyPasswordScript, username)
	cmd.Stdin = strings.NewReader(password + "\n")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		log.Printf("[terminal] validateCredentials: failed for user %q: %v (stderr: %s)", username, err, errMsg)
		return false
	}
	return true
}

func sendTermMsg(conn *websocket.Conn, msgType, data string) {
	msg := terminalMessage{Type: msgType, Data: data}
	websocket.JSON.Send(conn, msg)
}

func setTermSize(f *os.File, rows, cols uint16) {
	ws := struct {
		Row    uint16
		Col    uint16
		Xpixel uint16
		Ypixel uint16
	}{rows, cols, 0, 0}
	syscall.Syscall(
		syscall.SYS_IOCTL,
		f.Fd(),
		uintptr(syscall.TIOCSWINSZ),
		uintptr(unsafe.Pointer(&ws)),
	)
}
