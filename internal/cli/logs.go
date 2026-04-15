package cli

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

// NewLogsCmd returns the `apogee logs` subcommand: tail the daemon
// log files. No external dependencies — the reader is a simple
// seek-to-end + periodic-poll loop, which is fine for log files of
// the size apogee produces (a few MB).
func NewLogsCmd(stdout, stderr io.Writer) *cobra.Command {
	var (
		follow bool
		stream string
		lines  int
	)
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Tail the apogee daemon log files",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogs(cmd.Context().Done(), stdout, stderr, stream, lines, follow)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", true, "Follow new log lines; pass --follow=false for one-shot")
	cmd.Flags().StringVar(&stream, "stream", "both", "Which stream to tail: out | err | both")
	cmd.Flags().IntVarP(&lines, "lines", "n", 50, "Show the last N lines first")
	return cmd
}

// logPaths resolves the out/err log paths under ~/.apogee/logs,
// honouring a cfg override when present. Missing files are not an
// error here — runLogs checks existence and emits a friendly
// message when the daemon has never run.
func logPaths() (outPath, errPath string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	dir := filepath.Join(home, ".apogee", "logs")
	return filepath.Join(dir, "apogee.out.log"), filepath.Join(dir, "apogee.err.log"), nil
}

func runLogs(done <-chan struct{}, stdout, stderr io.Writer, stream string, lines int, follow bool) error {
	outPath, errPath, err := logPaths()
	if err != nil {
		return err
	}

	paths := []string{}
	switch stream {
	case "out":
		paths = append(paths, outPath)
	case "err":
		paths = append(paths, errPath)
	case "", "both":
		paths = append(paths, outPath, errPath)
	default:
		return fmt.Errorf("logs: invalid --stream %q (expected out|err|both)", stream)
	}

	// If neither file exists, the daemon has not produced any logs
	// yet. Emit the friendly onboarding message and exit cleanly.
	anyExists := false
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			anyExists = true
			break
		}
	}
	if !anyExists {
		fmt.Fprintln(stderr, "apogee logs: daemon is not installed. Run `apogee daemon install` first.")
		return nil
	}

	// Seed with the last N lines of each file.
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if len(paths) > 1 {
			fmt.Fprintf(stdout, "==> %s <==\n", p)
		}
		if err := writeTail(stdout, p, lines); err != nil {
			fmt.Fprintf(stderr, "apogee logs: %s: %v\n", p, err)
		}
	}

	if !follow {
		return nil
	}

	// Follow loop: open each file, seek to end, poll.
	return followLogs(done, stdout, stderr, paths)
}

// writeTail reads the last n lines of a file and writes them to w.
// Big files are NOT loaded into memory in full: we seek backwards
// in 8KiB chunks until we collect enough newlines.
func writeTail(w io.Writer, path string, n int) error {
	if n <= 0 {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	size := info.Size()
	if size == 0 {
		return nil
	}
	const chunk int64 = 8192
	var buf []byte
	var offset int64 = size
	var newlines int
	for offset > 0 && newlines <= n {
		readSize := chunk
		if offset < readSize {
			readSize = offset
		}
		offset -= readSize
		tmp := make([]byte, readSize)
		if _, err := f.ReadAt(tmp, offset); err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		buf = append(tmp, buf...)
		newlines = bytes.Count(buf, []byte("\n"))
	}
	// Trim the first (n+1) newlines off the front so we only print
	// the last n lines.
	if newlines > n {
		extra := newlines - n
		for i := 0; i < extra; i++ {
			idx := bytes.IndexByte(buf, '\n')
			if idx < 0 {
				break
			}
			buf = buf[idx+1:]
		}
	}
	_, err = w.Write(buf)
	return err
}

// followLogs opens each path, seeks to the end, and polls for new
// bytes every 200ms. Returns when done is closed.
func followLogs(done <-chan struct{}, stdout, stderr io.Writer, paths []string) error {
	type handle struct {
		path   string
		reader *bufio.Reader
		file   *os.File
	}
	handles := []*handle{}
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		if _, err := f.Seek(0, io.SeekEnd); err != nil {
			_ = f.Close()
			continue
		}
		handles = append(handles, &handle{path: p, reader: bufio.NewReader(f), file: f})
	}
	defer func() {
		for _, h := range handles {
			_ = h.file.Close()
		}
	}()

	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-done:
			return nil
		case <-tick.C:
			for _, h := range handles {
				for {
					line, err := h.reader.ReadString('\n')
					if len(line) > 0 {
						_, _ = stdout.Write([]byte(line))
					}
					if err != nil {
						break
					}
				}
			}
		}
	}
}
