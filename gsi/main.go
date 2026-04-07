// gsi — Dota 2 Game State Integration receiver.
// Listens for GSI payloads from the local Dota 2 client and prints them.
// Run this before launching a Dota 2 match to see what data is available.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

const (
	addr      = ":1337"
	authToken = "inhouse-dev"
)

func main() {
	log.Printf("GSI receiver listening on %s — start Dota 2 and enter a match", addr)

	http.HandleFunc("/gsi", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("read error: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Parse just enough to check auth and extract top-level keys.
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(body, &payload); err != nil {
			log.Printf("json parse error: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Verify auth token.
		if auth, ok := payload["auth"]; ok {
			var a struct {
				Token string `json:"token"`
			}
			if err := json.Unmarshal(auth, &a); err == nil && a.Token != authToken {
				log.Printf("bad auth token: %q", a.Token)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
		}

		// Pretty-print the full payload to a timestamped file and stdout.
		pretty, _ := json.MarshalIndent(payload, "", "  ")

		fmt.Printf("\n--- GSI payload @ %s ---\n", time.Now().Format("15:04:05"))
		fmt.Printf("sections present: ")
		for k := range payload {
			fmt.Printf("%s ", k)
		}
		fmt.Println()

		// Print each section separately so the output is readable.
		for _, section := range []string{"map", "player", "hero", "allplayers", "events", "draft"} {
			if data, ok := payload[section]; ok {
				fmt.Printf("\n[%s]\n%s\n", section, prettyJSON(data))
			}
		}

		// Also dump full payload to a file for closer inspection.
		filename := fmt.Sprintf("gsi_%s.json", time.Now().Format("150405"))
		if err := os.WriteFile(filename, pretty, 0644); err != nil {
			log.Printf("write file error: %v", err)
		} else {
			log.Printf("full payload saved to %s", filename)
		}

		w.WriteHeader(http.StatusOK)
	})

	log.Fatal(http.ListenAndServe(addr, nil))
}

func prettyJSON(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	out, _ := json.MarshalIndent(v, "", "  ")
	return string(out)
}
