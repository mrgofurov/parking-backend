package hikvision

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

const sampleXML = `<?xml version="1.0" encoding="UTF-8"?>
<EventNotificationAlert version="2.0" xmlns="http://www.isapi.org/ver20/XMLSchema">
<ipAddress>192.168.1.64</ipAddress>
<portNo>80</portNo>
<protocol>HTTP</protocol>
<macAddress>bc:ad:28:00:00:01</macAddress>
<channelID>1</channelID>
<dateTime>2026-11-25T17:30:00+05:00</dateTime>
<activePostCount>1</activePostCount>
<eventType>ANPR</eventType>
<eventState>active</eventState>
<eventDescription>ANPR</eventDescription>
<channelName>Kirish</channelName>
<ANPR>
<country>255</country>
<licensePlate>01A777AA</licensePlate>
<line>1</line>
<direction>forward</direction>
<confidenceLevel>97</confidenceLevel>
<plateType>unknown</plateType>
<vehicleType>vehicle</vehicleType>
</ANPR>
</EventNotificationAlert>`

func TestParseBareXML(t *testing.T) {
	evt, err := ParseEvent("application/xml", []byte(sampleXML))
	if err != nil {
		t.Fatal(err)
	}
	if evt.Plate != "01A777AA" || evt.Confidence != 97 || evt.IPAddress != "192.168.1.64" || evt.EventType != "ANPR" {
		t.Fatalf("unexpected event: %+v", evt)
	}
	if !evt.HasPlate() {
		t.Fatal("expected plate")
	}
}

func TestParseMultipartWithPictures(t *testing.T) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="anpr.xml"; filename="anpr.xml"`)
	h.Set("Content-Type", "application/xml")
	p, _ := w.CreatePart(h)
	_, _ = p.Write([]byte(sampleXML))

	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 1, 2, 3}
	for _, name := range []string{"licensePlatePicture.jpg", "detectionPicture.jpg"} {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, name, name))
		h.Set("Content-Type", "image/jpeg")
		p, _ := w.CreatePart(h)
		_, _ = p.Write(append([]byte{}, jpeg...))
	}
	_ = w.Close()

	evt, err := ParseEvent(w.FormDataContentType(), buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if evt.Plate != "01A777AA" {
		t.Fatalf("plate = %q", evt.Plate)
	}
	if evt.PlateImage == nil || evt.SceneImage == nil {
		t.Fatalf("pictures not extracted: plate=%v scene=%v", evt.PlateImage != nil, evt.SceneImage != nil)
	}
}

func TestParseJSONAndHeartbeat(t *testing.T) {
	evt, err := ParseEvent("application/json", []byte(`{"ipAddress":"10.0.0.5","eventType":"ANPR","ANPR":{"licensePlate":"40B123CD","confidenceLevel":88}}`))
	if err != nil || evt.Plate != "40B123CD" || evt.Confidence != 88 {
		t.Fatalf("json: %+v %v", evt, err)
	}

	hb := `<EventNotificationAlert><ipAddress>10.0.0.5</ipAddress><eventType>videoloss</eventType><eventState>inactive</eventState></EventNotificationAlert>`
	evt, err = ParseEvent("application/xml", []byte(hb))
	if err != nil || evt.HasPlate() {
		t.Fatalf("heartbeat must parse without plate: %+v %v", evt, err)
	}

	evt, _ = ParseEvent("application/xml", []byte(strings.Replace(sampleXML, "01A777AA", "unknown", 1)))
	if evt.HasPlate() {
		t.Fatal("'unknown' must not count as a plate")
	}
}

// Fake ISAPI device enforcing Digest auth.
func TestTriggerOutputDigest(t *testing.T) {
	const user, pass, realm, nonce = "admin", "secret", "DS-2CD", "abc123"
	PulseDuration = time.Millisecond
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Digest ") {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Digest qop="auth", realm="%s", nonce="%s", stale="FALSE"`, realm, nonce))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		p := parseChallenge(auth[len("Digest "):])
		sum := func(s string) string { h := md5.Sum([]byte(s)); return hex.EncodeToString(h[:]) }
		ha1 := sum(user + ":" + realm + ":" + pass)
		ha2 := sum(r.Method + ":" + p["uri"])
		want := sum(ha1 + ":" + nonce + ":" + p["nc"] + ":" + p["cnonce"] + ":" + p["qop"] + ":" + ha2)
		if p["response"] != want || p["uri"] != "/ISAPI/System/IO/outputs/1/trigger" || r.Method != http.MethodPut {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		_, _ = w.Write([]byte(`<ResponseStatus><statusCode>1</statusCode></ResponseStatus>`))
	}))
	defer srv.Close()

	if err := NewClient(srv.URL, user, pass).TriggerOutput(1); err != nil {
		t.Fatalf("trigger failed: %v", err)
	}
	// A pulse: "high" (body replayed after the Digest challenge), then "low"
	// so the camera does not latch the relay closed.
	if len(bodies) != 2 ||
		!strings.Contains(bodies[0], "<outputState>high</outputState>") ||
		!strings.Contains(bodies[1], "<outputState>low</outputState>") {
		t.Fatalf("want high then low, got %q", bodies)
	}
	if err := NewClient(srv.URL, user, "wrong").TriggerOutput(1); err == nil {
		t.Fatal("wrong password must fail")
	}
}
