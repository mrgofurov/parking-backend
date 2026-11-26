package hikvision

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// doDigest sends a request with HTTP Digest authentication (RFC 2617, MD5,
// qop=auth), which is what Hikvision ISAPI requires by default. Basic auth is
// used as a fallback when the device advertises it instead.
func doDigest(client *http.Client, method, url, username, password string, body []byte, contentType string) (*http.Response, error) {
	newReq := func() (*http.Request, error) {
		var r io.Reader
		if body != nil {
			r = bytes.NewReader(body)
		}
		req, err := http.NewRequest(method, url, r)
		if err != nil {
			return nil, err
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		return req, nil
	}

	req, err := newReq()
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}

	challenge := resp.Header.Get("WWW-Authenticate")
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	req, err = newReq()
	if err != nil {
		return nil, err
	}

	lower := strings.ToLower(challenge)
	switch {
	case strings.HasPrefix(lower, "digest"):
		params := parseChallenge(challenge[len("digest"):])
		req.Header.Set("Authorization", digestHeader(params, method, req.URL.RequestURI(), username, password))
	case strings.HasPrefix(lower, "basic"):
		req.SetBasicAuth(username, password)
	default:
		return nil, fmt.Errorf("qurilma noma'lum autentifikatsiya so'radi: %q", challenge)
	}
	return client.Do(req)
}

func parseChallenge(s string) map[string]string {
	params := map[string]string{}
	for _, part := range splitParams(s) {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		params[strings.ToLower(strings.TrimSpace(kv[0]))] = strings.Trim(strings.TrimSpace(kv[1]), `"`)
	}
	return params
}

// splitParams splits on commas that are not inside quotes.
func splitParams(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
			cur.WriteRune(r)
		case r == ',' && !inQuote:
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func md5hex(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

func digestHeader(p map[string]string, method, uri, username, password string) string {
	realm, nonce, opaque := p["realm"], p["nonce"], p["opaque"]
	ha1 := md5hex(username + ":" + realm + ":" + password)
	ha2 := md5hex(method + ":" + uri)

	qop := ""
	for _, q := range strings.Split(p["qop"], ",") {
		if strings.TrimSpace(q) == "auth" {
			qop = "auth"
		}
	}

	var response, cnonce string
	const nc = "00000001"
	if qop == "auth" {
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		cnonce = hex.EncodeToString(b)
		response = md5hex(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
	} else {
		response = md5hex(ha1 + ":" + nonce + ":" + ha2)
	}

	h := fmt.Sprintf(`Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s"`,
		username, realm, nonce, uri, response)
	if qop != "" {
		h += fmt.Sprintf(`, qop=%s, nc=%s, cnonce="%s"`, qop, nc, cnonce)
	}
	if opaque != "" {
		h += fmt.Sprintf(`, opaque="%s"`, opaque)
	}
	if alg := p["algorithm"]; alg != "" {
		h += fmt.Sprintf(`, algorithm=%s`, alg)
	}
	return h
}
