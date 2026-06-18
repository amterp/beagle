package runner

import (
	"io"
	"os"

	"github.com/amterp/beagle/internal/core"
)

// maxLogBytes caps each job log file. Past this, the oldest content is dropped.
const maxLogBytes int64 = 5 << 20 // 5 MiB

// RotateLogs trims a job's stdout/stderr logs if they have grown past the cap.
//
// launchd opens these files in append mode and holds the descriptor while the
// job runs, so renaming is useless (writes keep landing on the old inode). The
// one operation that cooperates with an append descriptor is truncate-in-place:
// we keep the most recent half and rewrite the file, after which launchd's
// append writes resume cleanly at the new end. Called before exec, so no child
// output races the rewrite. Best-effort: errors are ignored.
func RotateLogs(jobID string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	for _, stream := range []string{"stdout", "stderr"} {
		rotateLog(core.LogFilePath(home, jobID, stream))
	}
}

func rotateLog(path string) {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() <= maxLogBytes {
		return
	}
	keep := maxLogBytes / 2
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.Seek(-keep, io.SeekEnd); err != nil {
		return
	}
	tail, err := io.ReadAll(f)
	if err != nil {
		return
	}
	// O_TRUNC rewrites the same inode, so launchd's append descriptor stays
	// valid and resumes at the end of the kept tail.
	_ = os.WriteFile(path, tail, 0o644)
}
