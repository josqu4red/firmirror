package utils

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const maxRetries = 3

func DownloadFile(url string) (io.ReadCloser, error) {
	var resp *http.Response
	var err error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		resp, err = http.Get(url)
		if err != nil {
			if attempt < maxRetries {
				time.Sleep(time.Duration(attempt+1) * time.Second)
				continue
			}
			return nil, fmt.Errorf("failed after %d attempts: %w", maxRetries+1, err)
		}

		if resp.StatusCode == http.StatusOK {
			return resp.Body, nil
		}

		resp.Body.Close()

		// Retry on 5xx errors and rate limiting
		if (resp.StatusCode >= 500 || resp.StatusCode == 429) && attempt < maxRetries {
			time.Sleep(time.Duration(attempt+1) * time.Second)
			continue
		}

		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil, fmt.Errorf("failed after %d attempts: %w", maxRetries+1, err)
}

func DownloadFileToDest(url, file string) error {
	out, err := os.Create(file)
	if err != nil {
		return err
	}
	defer out.Close()

	resp, err := DownloadFile(url)
	if err != nil {
		return err
	}
	defer resp.Close()

	_, err = io.Copy(out, resp)
	return err
}
