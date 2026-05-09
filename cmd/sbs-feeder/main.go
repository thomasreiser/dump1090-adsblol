// Command sbs-feeder is a tiny TCP server that emits canned SBS BaseStation
// lines on connect. Useful for smoke-testing the main server without a real
// receiver — point ./server at the address sbs-feeder prints
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"time"
)

const defaultBatch = `MSG,1,1,1,A1B2C3,1,2026/05/22,12:00:00.000,2026/05/22,12:00:00.000,AAL1234 ,,,,,,,,,,,
MSG,3,1,1,A1B2C3,1,2026/05/22,12:00:00.500,2026/05/22,12:00:00.500,,35000,,,40.7128,-74.0060,,,0,0,0,0
MSG,4,1,1,A1B2C3,1,2026/05/22,12:00:01.000,2026/05/22,12:00:01.000,,,450.2,90.0,,,-256,,,,,
MSG,6,1,1,A1B2C3,1,2026/05/22,12:00:01.500,2026/05/22,12:00:01.500,,35000,,,,,,1234,0,0,0,0
MSG,1,2,2,DEF456,1,2026/05/22,12:00:02.000,2026/05/22,12:00:02.000,UAL777  ,,,,,,,,,,,
MSG,3,2,2,DEF456,1,2026/05/22,12:00:02.500,2026/05/22,12:00:02.500,,38000,,,33.9425,-118.4081,,,0,0,0,0
MSG,6,2,2,DEF456,1,2026/05/22,12:00:03.000,2026/05/22,12:00:03.000,,38000,,,,,,7700,-1,-1,0,0
MSG,3,3,3,AE1234,1,2026/05/22,12:00:03.500,2026/05/22,12:00:03.500,,20000,,,34.0,-118.0,,,0,0,0,0
MSG,3,4,4,ADF001,1,2026/05/22,12:00:04.000,2026/05/22,12:00:04.000,,10000,,,40.0,-75.0,,,0,0,0,0
`

func main() {
	addr := flag.String("addr", ":30003", "listen address")
	interval := flag.Duration("interval", 500*time.Millisecond, "delay between bursts after the first batch")
	flag.Parse()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen %s: %v", *addr, err)
	}
	fmt.Fprintf(os.Stderr, "sbs-feeder: listening on %s\n", ln.Addr())
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Fatalf("accept: %v", err)
		}
		go serve(conn, *interval)
	}
}

func serve(conn net.Conn, interval time.Duration) {
	defer conn.Close()
	if _, err := conn.Write([]byte(defaultBatch)); err != nil {
		return
	}
	for {
		time.Sleep(interval)
		now := time.Now().UTC()
		line := fmt.Sprintf(
			"MSG,5,1,1,A1B2C3,1,%s,%s,%s,%s,,%d,,,,,,,,,,\r\n",
			now.Format("2006/01/02"), now.Format("15:04:05.000"),
			now.Format("2006/01/02"), now.Format("15:04:05.000"),
			35000,
		)
		if _, err := conn.Write([]byte(line)); err != nil {
			return
		}
	}
}
