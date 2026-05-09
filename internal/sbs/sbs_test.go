package sbs

import (
	"testing"
)

func TestParse_Identification(t *testing.T) {
	line := "MSG,1,1,1,A1B2C3,1,2024/01/01,12:00:00.000,2024/01/01,12:00:00.000,AAL123  ,,,,,,,,,,,"
	m, err := Parse(line)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.Type != MsgESIdentification {
		t.Errorf("type = %d, want %d", m.Type, MsgESIdentification)
	}
	if m.HexIdent != "a1b2c3" {
		t.Errorf("hex = %q, want a1b2c3", m.HexIdent)
	}
	if m.Callsign == nil || *m.Callsign != "AAL123" {
		t.Errorf("callsign = %v, want AAL123", m.Callsign)
	}
	if m.Altitude != nil {
		t.Errorf("altitude = %v, want nil", m.Altitude)
	}
}

func TestParse_AirbornePosition(t *testing.T) {
	line := "MSG,3,1,1,A1B2C3,1,2024/01/01,12:00:00.000,2024/01/01,12:00:00.000,,35000,,,40.7128,-74.0060,,,0,0,0,0"
	m, err := Parse(line)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.Altitude == nil || *m.Altitude != 35000 {
		t.Errorf("altitude = %v, want 35000", m.Altitude)
	}
	if m.Latitude == nil || *m.Latitude != 40.7128 {
		t.Errorf("lat = %v, want 40.7128", m.Latitude)
	}
	if m.Longitude == nil || *m.Longitude != -74.0060 {
		t.Errorf("lon = %v, want -74.0060", m.Longitude)
	}
	if m.OnGround == nil || *m.OnGround {
		t.Errorf("onground = %v, want false", m.OnGround)
	}
}

func TestParse_AirborneVelocity(t *testing.T) {
	line := "MSG,4,1,1,A1B2C3,1,2024/01/01,12:00:00.000,2024/01/01,12:00:00.000,,,450.2,180.5,,,-1024,,,,,"
	m, err := Parse(line)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.GroundSpeed == nil || *m.GroundSpeed != 450.2 {
		t.Errorf("gs = %v, want 450.2", m.GroundSpeed)
	}
	if m.Track == nil || *m.Track != 180.5 {
		t.Errorf("track = %v, want 180.5", m.Track)
	}
	if m.VerticalRate == nil || *m.VerticalRate != -1024 {
		t.Errorf("vr = %v, want -1024", m.VerticalRate)
	}
}

func TestParse_SurveillanceID(t *testing.T) {
	line := "MSG,6,1,1,A1B2C3,1,2024/01/01,12:00:00.000,2024/01/01,12:00:00.000,,2500,,,,,,7700,-1,-1,0,0"
	m, err := Parse(line)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.Squawk == nil || *m.Squawk != "7700" {
		t.Errorf("squawk = %v, want 7700", m.Squawk)
	}
	if m.Alert == nil || !*m.Alert {
		t.Errorf("alert = %v, want true", m.Alert)
	}
	if m.Emergency == nil || !*m.Emergency {
		t.Errorf("emergency = %v, want true", m.Emergency)
	}
}

func TestParse_OnGround(t *testing.T) {
	line := "MSG,8,1,1,A1B2C3,1,2024/01/01,12:00:00.000,2024/01/01,12:00:00.000,,,,,,,,,,,,-1"
	m, err := Parse(line)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.OnGround == nil || !*m.OnGround {
		t.Errorf("onground = %v, want true", m.OnGround)
	}
}

func TestParse_NotMSG(t *testing.T) {
	_, err := Parse("STA,,,,A1B2C3,,2024/01/01,12:00:00.000,2024/01/01,12:00:00.000,,,,,,,,,,,,")
	if err != ErrNotMSG {
		t.Errorf("err = %v, want ErrNotMSG", err)
	}
}

func TestParse_Short(t *testing.T) {
	if _, err := Parse("MSG,1,1"); err == nil {
		t.Error("expected error on short record")
	}
}

func TestParse_BadHex(t *testing.T) {
	if _, err := Parse("MSG,1,1,1,SHORT,1,,,,,XX,,,,,,,,,,,"); err == nil {
		t.Error("expected error on short hex")
	}
}

func TestParse_TrimsAndLowercasesHex(t *testing.T) {
	m, err := Parse("MSG,5,1,1,AABBCC,1,,,,,,30000,,,,,,,,,,")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.HexIdent != "aabbcc" {
		t.Errorf("hex = %q, want aabbcc", m.HexIdent)
	}
}

func TestParse_HandlesCRLF(t *testing.T) {
	m, err := Parse("MSG,5,1,1,AABBCC,1,,,,,,30000,,,,,,,,,,\r\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.Altitude == nil || *m.Altitude != 30000 {
		t.Errorf("altitude = %v, want 30000", m.Altitude)
	}
}
