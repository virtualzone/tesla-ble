package main

import (
	"io"
	"net/http"
	"os"
)

func GetCacheHttpFile(url string, localFile string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	resBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if err := os.WriteFile(localFile, resBody, 0644); err != nil {
		return err
	}
	return nil
}
