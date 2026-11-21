// Package branding provides the display identity shared by the server and web app.
package branding

import (
	_ "embed"
	"encoding/json"
)

//go:embed brand.json
var config []byte

type displayConfig struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

var display = loadDisplay()

func loadDisplay() displayConfig {
	var display displayConfig
	if err := json.Unmarshal(config, &display); err != nil {
		panic(err)
	}
	return display
}

// Name is the product name shown in user interfaces and messages.
func Name() string { return display.Name }

// Description is the short product description shown to users.
func Description() string { return display.Description }
