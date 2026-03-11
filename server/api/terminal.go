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

// verifyPasswordScript reads the password from stdin and verifies it against
// /etc/shadow using Python's crypt module. The username is passed as argv[1].
const verifyPasswordScript = `
import crypt, sys
password = sys.stdin.readline().rstrip('\n')
username = sys.argv[1]
with open('/etc/shadow') as f:
    for line in f:
        parts = line.strip().split(':')
        if parts[0] == username:
            stored = parts[1]
            if not stored or stored in ('*', '!!') or stored.startswith('!'):
                sys.exit(1)
            if crypt.crypt(password, stored) == stored:
                sys.exit(0)
            sys.exit(1)
sys.exit(1)
`

// validateCredentials checks if the username/password is valid by reading
// /etc/shadow and verifying the password hash. The server runs as root so
// it has direct access to the shadow file.
func validateCredentials(username, password string) bool {
	// Check that the user exists on the system
	if _, err := user.Lookup(username); err != nil {
		log.Printf("[terminal] validateCredentials: user %q not found: %v", username, err)
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Password is passed via stdin to avoid exposing it in /proc/PID/cmdline.
	cmd := exec.CommandContext(ctx, "python3", "-W", "ignore::DeprecationWarning", "-c", verifyPasswordScript, username)
	cmd.Stdin = strings.NewReader(password + "\n")
	if err := cmd.Run(); err != nil {
		log.Printf("[terminal] validateCredentials: failed for user %q: %v", username, err)
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
