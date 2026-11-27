package domain

import "testing"

func TestEffectiveRTSPURL(t *testing.T) {
	cases := []struct {
		name string
		cam  Camera
		want string
	}{
		{"explicit URL wins", Camera{RTSPUrl: " rtsp://x/y ", IPAddress: "192.168.1.64"}, "rtsp://x/y"},
		{"built with defaults", Camera{IPAddress: "192.168.1.64", Username: "admin", Password: "secret"},
			"rtsp://admin:secret@192.168.1.64:554/Streaming/Channels/102"},
		{"custom port and main stream", Camera{IPAddress: "10.0.0.5", RTSPPort: 8554, StreamChannel: 101, Username: "admin", Password: "p"},
			"rtsp://admin:p@10.0.0.5:8554/Streaming/Channels/101"},
		{"special characters are escaped", Camera{IPAddress: "192.168.1.64", Username: "admin", Password: "a@b:c/d#"},
			"rtsp://admin:a%40b%3Ac%2Fd%23@192.168.1.64:554/Streaming/Channels/102"},
		{"no login", Camera{IPAddress: "192.168.1.64"}, "rtsp://192.168.1.64:554/Streaming/Channels/102"},
		{"no IP, no URL", Camera{}, ""},
	}
	for _, c := range cases {
		if got := c.cam.EffectiveRTSPURL(); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
