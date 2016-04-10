package gode

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dickeyxxx/golock"
	"github.com/franela/goreq"
	"github.com/mitchellh/ioprogress"
	"github.com/ulikunitz/xz"
)

var errInvalidSha = errors.New("Invalid SHA")

// IsSetup returns true if node is setup in RootPath
func IsSetup() (bool, error) {
	exists, err := fileExists(nodeBinPath)
	if !exists {
		return exists, err
	}
	return fileExists(npmBinPath)
}

// Setup downloads and sets up node in the RootPath directory
func Setup() error {
	golock.Lock(lockPath)
	defer golock.Unlock(lockPath)
	if setup, _ := IsSetup(); setup {
		return nil
	}
	if exists, _ := fileExists(nodeBinPath); !exists {
		if err := downloadFile(nodeBinPath, t.URL, t.Sha); err != nil {
			return err
		}
		if err := os.Chmod(nodeBinPath, 0755); err != nil {
			return err
		}
		if err := checkNodeVersion(); err != nil {
			os.Remove(nodeBinPath)
			return err
		}
	}
	if exists, _ := fileExists(npmBinPath); !exists {
		if err := downloadNpm(); err != nil {
			return err
		}
		if err := checkNpmVersion(); err != nil {
			os.RemoveAll(npmBasePath)
			return err
		}
	}
	if err := os.MkdirAll(modulesDir, 0755); err != nil {
		return err
	}
	clearOldNodeInstalls()
	return nil
}

func downloadNpm() error {
	reader, getSha, err := downloadXZ(npmURL)
	if err != nil {
		return err
	}
	tmpDir := tmpDir()

	if err := extractTar(reader, tmpDir); err != nil {
		return err
	}
	if getSha() != npmSha {
		return errInvalidSha
	}
	os.RemoveAll(filepath.Join(npmBasePath))
	os.Rename(filepath.Join(tmpDir, "npm-"+NpmVersion), npmBasePath)
	return os.RemoveAll(tmpDir)
}

func downloadXZ(url string) (io.Reader, func() string, error) {
	req := goreq.Request{Uri: url, Timeout: 30 * time.Minute}
	resp, err := req.Do()
	if err != nil {
		return nil, nil, err
	}
	if err := getHTTPError(resp); err != nil {
		return nil, nil, err
	}
	size, _ := strconv.Atoi(resp.Header.Get("Content-Length"))
	progress := &ioprogress.Reader{
		Reader:   resp.Body,
		Size:     int64(size),
		DrawFunc: ioprogress.DrawTerminalf(os.Stderr, progressDrawFn),
	}
	getSha, reader := computeSha(progress)
	uncompressed, err := xz.NewReader(reader)
	return uncompressed, getSha, err
}

func downloadFile(path, url, sha string) error {
	reader, getSha, err := downloadXZ(url)
	if err != nil {
		return err
	}
	tmp := filepath.Join(tmpDir(), "file")
	file, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err = io.Copy(file, reader); err != nil {
		return err
	}
	file.Close()
	if getSha() != sha {
		return errInvalidSha
	}
	if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	os.Remove(path)
	err = os.Rename(tmp, path)
	if err != nil {
		return err
	}
	return os.RemoveAll(filepath.Dir(tmp))
}

func progressDrawFn(progress, total int64) string {
	return fmt.Sprintf("heroku-cli: Adding dependencies... %15s", ioprogress.DrawTextFormatBytes(progress, total))
}

func clearOldNodeInstalls() {
	for _, name := range getDirsWithPrefix(rootPath, "node-") {
		if !strings.HasPrefix(name, "node-"+NodeVersion) {
			os.RemoveAll(filepath.Join(rootPath, name))
		}
	}
	for _, name := range getDirsWithPrefix(rootPath, "npm-") {
		if name != "npm-"+NpmVersion {
			os.RemoveAll(filepath.Join(rootPath, name))
		}
	}
}

func getHTTPError(resp *goreq.Response) error {
	if resp.StatusCode < 400 {
		return nil
	}
	var body string
	body = resp.Header.Get("Content-Type")
	return fmt.Errorf("%s: %s", resp.Status, body)
}

func checkNodeVersion() error {
	cmd, done := RunScript("process.stdout.write(process.version)")
	output, err := cmd.CombinedOutput()
	done()
	if err != nil {
		return err
	}
	if string(output) != "v"+NodeVersion {
		return fmt.Errorf("expected node %s to equal %s", string(output), "v"+NodeVersion)
	}
	return nil
}

func checkNpmVersion() error {
	stdout, stderr, err := execNpm("-v")
	if err != nil {
		return err
	}
	if stdout != NpmVersion+"\n" {
		return fmt.Errorf("expected npm %s to equal %s\n%s", stdout, "v"+NodeVersion, stderr)
	}
	return nil
}
