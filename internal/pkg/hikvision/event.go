package hikvision

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"strconv"
	"strings"
)

// ANPREvent is the normalised content of a Hikvision "EventNotificationAlert"
// sent by an ANPR/LPR camera through HTTP Listening (alarm server).
type ANPREvent struct {
	IPAddress   string
	MacAddress  string
	ChannelID   string
	ChannelName string
	EventType   string
	EventState  string
	DateTime    string
	Plate       string
	Confidence  float64
	Direction   string // camera-side detection direction (forward / reverse), informational
	VehicleType string

	PlateImage  []byte // licensePlatePicture.jpg (crop)
	SceneImage  []byte // detectionPicture.jpg (full frame)
	RawMetadata string
}

// HasPlate reports whether the event carries a usable plate.
func (e *ANPREvent) HasPlate() bool {
	p := strings.TrimSpace(strings.ToLower(e.Plate))
	switch p {
	case "", "unknown", "noplate", "no plate", "无车牌", "车牌", "-":
		return false
	}
	return true
}

type xmlAlert struct {
	XMLName     xml.Name `xml:"EventNotificationAlert"`
	IPAddress   string   `xml:"ipAddress"`
	IPv6Address string   `xml:"ipv6Address"`
	MacAddress  string   `xml:"macAddress"`
	ChannelID   string   `xml:"channelID"`
	ChannelName string   `xml:"channelName"`
	DateTime    string   `xml:"dateTime"`
	EventType   string   `xml:"eventType"`
	EventState  string   `xml:"eventState"`
	ANPR        struct {
		LicensePlate    string `xml:"licensePlate"`
		OriginalPlate   string `xml:"originalLicensePlate"`
		ConfidenceLevel string `xml:"confidenceLevel"`
		Direction       string `xml:"direction"`
		VehicleType     string `xml:"vehicleType"`
	} `xml:"ANPR"`
}

type jsonAlert struct {
	IPAddress   string `json:"ipAddress"`
	MacAddress  string `json:"macAddress"`
	ChannelID   any    `json:"channelID"`
	ChannelName string `json:"channelName"`
	DateTime    string `json:"dateTime"`
	EventType   string `json:"eventType"`
	EventState  string `json:"eventState"`
	ANPR        *struct {
		LicensePlate    string `json:"licensePlate"`
		ConfidenceLevel any    `json:"confidenceLevel"`
		Direction       string `json:"direction"`
		VehicleType     string `json:"vehicleType"`
	} `json:"ANPR"`
}

// ParseEvent decodes the body of an HTTP Listening push. Hikvision sends either
// multipart/form-data (XML/JSON metadata + JPEG pictures) or a bare XML/JSON
// document (heartbeats, cameras with picture upload disabled).
func ParseEvent(contentType string, body []byte) (*ANPREvent, error) {
	mediaType, params, _ := mime.ParseMediaType(contentType)
	if strings.HasPrefix(mediaType, "multipart/") && params["boundary"] != "" {
		return parseMultipart(body, params["boundary"])
	}
	return parseMetadata(body)
}

func parseMultipart(body []byte, boundary string) (*ANPREvent, error) {
	mr := multipart.NewReader(bytes.NewReader(body), boundary)
	var evt *ANPREvent
	images := map[string][]byte{}
	var firstImage []byte

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			if evt != nil {
				break // keep what we have; some firmwares end the stream sloppily
			}
			return nil, err
		}
		data, _ := io.ReadAll(io.LimitReader(part, 10<<20))
		name := strings.ToLower(part.FileName())
		if name == "" {
			name = strings.ToLower(part.FormName())
		}
		ct := strings.ToLower(part.Header.Get("Content-Type"))

		isImage := strings.HasPrefix(ct, "image/") || strings.HasSuffix(name, ".jpg") || strings.HasSuffix(name, ".jpeg") ||
			(len(data) > 3 && data[0] == 0xFF && data[1] == 0xD8)
		if isImage {
			images[name] = data
			if firstImage == nil {
				firstImage = data
			}
			continue
		}
		if e, err := parseMetadata(data); err == nil {
			evt = e
		}
	}

	if evt == nil {
		return nil, errors.New("multipart ichida ANPR metadata topilmadi")
	}
	for name, img := range images {
		switch {
		case strings.Contains(name, "licenseplate") || strings.Contains(name, "plate"):
			evt.PlateImage = img
		case strings.Contains(name, "detection") || strings.Contains(name, "scene") || strings.Contains(name, "vehicle"):
			evt.SceneImage = img
		}
	}
	if evt.SceneImage == nil {
		evt.SceneImage = firstImage
	}
	return evt, nil
}

func parseMetadata(data []byte) (*ANPREvent, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, errors.New("bo'sh metadata")
	}

	if trimmed[0] == '{' {
		var j jsonAlert
		if err := json.Unmarshal(trimmed, &j); err != nil {
			return nil, err
		}
		evt := &ANPREvent{
			IPAddress:   j.IPAddress,
			MacAddress:  j.MacAddress,
			ChannelID:   anyToString(j.ChannelID),
			ChannelName: j.ChannelName,
			EventType:   j.EventType,
			EventState:  j.EventState,
			DateTime:    j.DateTime,
			RawMetadata: string(trimmed),
		}
		if j.ANPR != nil {
			evt.Plate = strings.TrimSpace(j.ANPR.LicensePlate)
			evt.Confidence = parseFloat(anyToString(j.ANPR.ConfidenceLevel))
			evt.Direction = j.ANPR.Direction
			evt.VehicleType = j.ANPR.VehicleType
		}
		return evt, nil
	}

	var x xmlAlert
	if err := xml.Unmarshal(trimmed, &x); err != nil {
		return nil, err
	}
	plate := strings.TrimSpace(x.ANPR.LicensePlate)
	if plate == "" {
		plate = strings.TrimSpace(x.ANPR.OriginalPlate)
	}
	ip := x.IPAddress
	if ip == "" {
		ip = x.IPv6Address
	}
	return &ANPREvent{
		IPAddress:   ip,
		MacAddress:  x.MacAddress,
		ChannelID:   x.ChannelID,
		ChannelName: x.ChannelName,
		EventType:   x.EventType,
		EventState:  x.EventState,
		DateTime:    x.DateTime,
		Plate:       plate,
		Confidence:  parseFloat(x.ANPR.ConfidenceLevel),
		Direction:   x.ANPR.Direction,
		VehicleType: x.ANPR.VehicleType,
		RawMetadata: string(trimmed),
	}, nil
}

func anyToString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}
