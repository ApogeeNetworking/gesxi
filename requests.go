package gesxi

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"time"
)

type Service struct {
	http    *http.Client
	BaseURL string
}

func newHttpService(host string, jar *http.CookieJar) *Service {
	return &Service{
		BaseURL: fmt.Sprintf("https://%s/folder", host),
		http: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
			Timeout: 12000 * time.Second,
			Jar:     *jar,
		},
	}
}

func (s *Service) GenerateRequest(method, url string, file *os.File) (*http.Request, error) {
	return http.NewRequest(method, url, file)
}

func (s *Service) MakeRequest(req *http.Request) (*http.Response, error) {
	return s.http.Do(req)
}
