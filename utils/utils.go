package utils

import (
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
)

// GetTmpDir creates a temporary directory and returns its path
// You need to manually empty and remove the directory when you're done with it
func GetTmpDir() (string, error) {
	tmpDir, err := os.MkdirTemp("", "fwupd-*")
	if err != nil {
		return "", err
	}
	return tmpDir, nil
}

func ReaderToFile(src io.Reader, dst string) error {
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	buffer := make([]byte, 4096)

	_, err = io.CopyBuffer(out, src, buffer)
	if err != nil {
		return err
	}

	return nil
}

func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	err = ReaderToFile(in, dst)
	if err != nil {
		return err
	}

	return nil
}

func GetFileFromName(name string, zipReader *zip.ReadCloser) (*zip.File, error) {
	for _, f := range zipReader.File {
		if f.Name == name {
			return f, nil
		}
	}
	return nil, fmt.Errorf("file not found: %s", name)
}

func GzipUnpack(src io.Reader) (io.ReadCloser, error) {
	gzipReader, err := gzip.NewReader(src)
	if err != nil {
		return nil, err
	}

	gzipReader.Multistream(false)
	return gzipReader, nil
}

func DownloadFile(url string) (io.ReadCloser, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return resp.Body, nil
}
