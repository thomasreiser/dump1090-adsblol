// Package sbs decodes the BaseStation (SBS-1) text format that dump1090
// exposes on TCP port 30003. Each line is a comma-separated record; see
// http://woodair.net/sbs/article/barebones42_socket_data.htm for the canonical
// field layout. We only care about MSG records.
package sbs

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// MessageType is the SBS "transmission type" — field 2 of a MSG record.
type MessageType int

const (
	MsgESIdentification   MessageType = 1 // callsign
	MsgESSurfacePosition  MessageType = 2 // surface lat/lon/gs/track
	MsgESAirbornePosition MessageType = 3 // airborne lat/lon, altitude
	MsgESAirborneVelocity MessageType = 4 // gs, track, vertical rate
	MsgSurveillanceAlt    MessageType = 5 // altitude only
	MsgSurveillanceID     MessageType = 6 // altitude + squawk
	MsgAirToAir           MessageType = 7 // altitude
	MsgAllCallReply       MessageType = 8 // ground bit only
)

// Message is a parsed SBS MSG line. Pointer fields distinguish "absent from
// this message" from "zero value".
type Message struct {
	Type      MessageType
	HexIdent  string
	Generated time.Time

	Callsign     *string
	Altitude     *int32
	GroundSpeed  *float64
	Track        *float64
	Latitude     *float64
	Longitude    *float64
	VerticalRate *int32
	Squawk       *string
	Alert        *bool
	Emergency    *bool
	SPI          *bool
	OnGround     *bool
}

// ErrNotMSG is returned for non-MSG lines (STA/SEL/AIR/ID/CLK) which we ignore.
var ErrNotMSG = errors.New("sbs: not a MSG record")

// Parse parses a single SBS BaseStation line.
func Parse(line string) (*Message, error) {
	line = strings.TrimRight(line, "\r\n ")
	if line == "" {
		return nil, errors.New("sbs: empty line")
	}
	fields := strings.Split(line, ",")
	if len(fields) < 22 {
		return nil, errors.New("sbs: short record")
	}
	if fields[0] != "MSG" {
		return nil, ErrNotMSG
	}
	tt, err := strconv.Atoi(fields[1])
	if err != nil {
		return nil, errors.New("sbs: bad transmission type")
	}
	if tt < 1 || tt > 8 {
		return nil, errors.New("sbs: unknown transmission type")
	}
	hex := strings.ToLower(strings.TrimSpace(fields[4]))
	if len(hex) != 6 {
		return nil, errors.New("sbs: bad hex ident")
	}

	m := &Message{
		Type:      MessageType(tt),
		HexIdent:  hex,
		Generated: parseDateTime(fields[6], fields[7]),
	}

	if s := strings.TrimSpace(fields[10]); s != "" {
		// Preserve trailing whitespace on the callsign as adsb.lol does (the
		// Mode-S frame pads to 8 chars). We *trim* here for whitespace-only
		// detection but assign the original.
		c := strings.TrimRight(fields[10], " ")
		m.Callsign = &c
	}
	if v, ok := parseInt32(fields[11]); ok {
		m.Altitude = &v
	}
	if v, ok := parseFloat(fields[12]); ok {
		m.GroundSpeed = &v
	}
	if v, ok := parseFloat(fields[13]); ok {
		m.Track = &v
	}
	if v, ok := parseFloat(fields[14]); ok {
		m.Latitude = &v
	}
	if v, ok := parseFloat(fields[15]); ok {
		m.Longitude = &v
	}
	if v, ok := parseInt32(fields[16]); ok {
		m.VerticalRate = &v
	}
	if s := strings.TrimSpace(fields[17]); s != "" {
		m.Squawk = &s
	}
	if v, ok := parseBool(fields[18]); ok {
		m.Alert = &v
	}
	if v, ok := parseBool(fields[19]); ok {
		m.Emergency = &v
	}
	if v, ok := parseBool(fields[20]); ok {
		m.SPI = &v
	}
	if v, ok := parseBool(fields[21]); ok {
		m.OnGround = &v
	}
	return m, nil
}

func parseInt32(s string) (int32, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return 0, false
	}
	return int32(v), true
}

func parseFloat(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func parseBool(s string) (bool, bool) {
	s = strings.TrimSpace(s)
	switch s {
	case "":
		return false, false
	case "0":
		return false, true
	case "-1", "1":
		return true, true
	}
	return false, false
}

// dump1090 emits "YYYY/MM/DD" and "HH:MM:SS.mmm". Parse leniently; if either
// half is unparseable we fall back to time.Time{} so callers can substitute
// time.Now().
func parseDateTime(date, t string) time.Time {
	date = strings.TrimSpace(date)
	t = strings.TrimSpace(t)
	if date == "" || t == "" {
		return time.Time{}
	}
	v, err := time.Parse("2006/01/02 15:04:05.000", date+" "+t)
	if err != nil {
		return time.Time{}
	}
	return v
}
