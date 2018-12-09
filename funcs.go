package ant

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"time"

	uuid "github.com/satori/go.uuid"
)

// isDir return true if path is a dir
func isDir(path string) bool {
	f, err := os.Stat(path)
	if err != nil {
		return false
	}
	if m := f.Mode(); m.IsDir() && m&400 != 0 {
		return true
	}
	return false
}

// isFile return true if path is a regular file
func isFile(path string) bool {
	f, err := os.Stat(path)
	if err != nil {
		return false
	}
	if m := f.Mode(); !m.IsDir() && m.IsRegular() && m&400 != 0 {
		return true
	}
	return false
}

func GetID(file string) (string, error) {
	if isFile(file) {
		id, err := ioutil.ReadFile(file)
		if err == nil {
			id = bytes.TrimSpace(id)
			if len(id) > 0 {
				if len(id) > 36 {
					return string(id[:36]), nil
				}
				return string(id), nil
			}
		}
	}
	uuid1, err := uuid.NewV1()
	if err != nil {
		return "", fmt.Errorf("could not create UUID, %s", err)
	}
	err = ioutil.WriteFile(file, []byte(uuid1.String()), 0644)
	if err != nil {
		return "", err
	}
	return uuid1.String(), nil
}

// GetHome returns the $HOME/.marabunta
func GetHome() (string, error) {
	home := os.Getenv("HOME")
	if home == "" {
		usr, err := user.Current()
		if err != nil {
			return "", fmt.Errorf("error getting user home: %s", err)
		}
		home = usr.HomeDir
	}
	home = filepath.Join(home, ".marabunta")
	if err := os.MkdirAll(home, os.ModePerm); err != nil {
		return "", err
	}
	return home, nil
}

func RequestCertificate(url, home, id string, data []byte) error {
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		return err
	}

	req.Header.Set("User-Agent", fmt.Sprintf("ant-%s", id))

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	client := &http.Client{
		Timeout:   time.Second * 10,
		Transport: tr,
	}

	res, err := client.Do(req)
	if err != nil {
		return err
	}

	defer res.Body.Close()

	b, err := ioutil.ReadAll(io.LimitReader(res.Body, 4096))
	if err != nil {
		return err
	}

	if res.StatusCode != 200 {
		return fmt.Errorf("%d - %s", res.StatusCode, b)
	}

	var blocks []byte
	for {
		var block *pem.Block
		block, b = pem.Decode(b)
		if block == nil {
			return fmt.Errorf("failed to parse certificate PEM")
		}
		blocks = append(blocks, block.Bytes...)
		if len(b) == 0 {
			break
		}
	}

	certs, err := x509.ParseCertificates(blocks)
	if err != nil {
		return err
	}

	if len(certs) < 2 {
		return fmt.Errorf("crt must have 2 concatenated certificates: client + CA")
	}

	// use the CA always as the last certificate
	// need to test for intermediate  certificates
	out := make([]*pem.Block, len(certs))
	for k, cert := range certs {
		block := &pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}
		if bytes.Compare(cert.RawIssuer, cert.RawSubject) == 0 && cert.IsCA {
			out[len(certs)-1] = block
		} else {
			out[k] = block
		}
	}

	file, err := os.OpenFile(filepath.Join(home, "ant.crt"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
	if err != nil {
		return err
	}

	for _, c := range out {
		pem.Encode(file, c)
	}

	return file.Close()
}
