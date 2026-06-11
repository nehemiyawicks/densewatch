package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"
)

// basicAuthTransport adds HTTP Basic auth to every request. Real Redfish BMCs and
// CDUs require authentication, and the @odata.id links we follow must carry it too.
type basicAuthTransport struct {
	base       http.RoundTripper
	user, pass string
}

func (t *basicAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.user != "" {
		req.SetBasicAuth(t.user, t.pass)
	}
	return t.base.RoundTrip(req)
}

// baseTransport builds a TLS-configured transport. Real BMCs serve HTTPS, often
// with self-signed certificates, so we accept a CA bundle or an explicit skip.
func baseTransport(caCert string, insecure bool) (*http.Transport, error) {
	tlsCfg := &tls.Config{InsecureSkipVerify: insecure}
	if caCert != "" {
		pem, err := os.ReadFile(caCert)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("no certificates found in %s", caCert)
		}
		tlsCfg.RootCAs = pool
	}
	return &http.Transport{TLSClientConfig: tlsCfg}, nil
}

// redfishCreds extracts Basic-auth credentials from a target URL's userinfo
// (https://user:pass@host/...), falling back to the REDFISH_USERNAME /
// REDFISH_PASSWORD environment variables, and returns the credential-stripped URL.
func redfishCreds(rawurl string) (cleanURL, user, pass string, err error) {
	u, perr := url.Parse(rawurl)
	if perr != nil {
		// Do not echo the raw URL or the parse error: either may carry credentials.
		return "", "", "", errors.New("invalid Redfish URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", "", "", fmt.Errorf("Redfish URL must use http or https (got scheme %q)", u.Scheme)
	}
	if u.Host == "" {
		return "", "", "", errors.New("Redfish URL has no host")
	}
	if u.User != nil {
		user = u.User.Username()
		pass, _ = u.User.Password()
		u.User = nil
	} else {
		user = os.Getenv("REDFISH_USERNAME")
		pass = os.Getenv("REDFISH_PASSWORD")
	}
	return u.String(), user, pass, nil
}

// redfishHTTPClient builds the HTTP client for one Redfish target: a shared TLS
// transport wrapped with per-target Basic auth applied to every request.
func redfishHTTPClient(timeout time.Duration, tr *http.Transport, user, pass string) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: &basicAuthTransport{base: tr, user: user, pass: pass},
	}
}
