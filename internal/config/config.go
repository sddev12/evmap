package config

import (
	"errors"
	"fmt"
	"strings"
)

var Config AppConfig

var keyCodeRegistry = map[string]uint16{
	"a": 30, "b": 48, "c": 46, "d": 32, "e": 18, "f": 33, "g": 34, "h": 35, "i": 23, "j": 36, "k": 37, "l": 38, "m": 50,
	"n": 49, "o": 24, "p": 25, "q": 16, "r": 19, "s": 31, "t": 20, "u": 22, "v": 47, "w": 17, "x": 45, "y": 21, "z": 44,
	"0": 11, "1": 2, "2": 3, "3": 4, "4": 5, "5": 6, "6": 7, "7": 8, "8": 9, "9": 10,
	"up": 103, "down": 108, "left": 105, "right": 106,
	"enter": 28, "space": 57, "tab": 15, "esc": 1, "backspace": 14, "delete": 111,
	"home": 102, "end": 107, "pageup": 104, "pagedown": 109,
	"f1": 59, "f2": 60, "f3": 61, "f4": 62, "f5": 63, "f6": 64, "f7": 65, "f8": 66, "f9": 67, "f10": 68, "f11": 87, "f12": 88,
	"lshift": 42, "rshift": 54, "lctrl": 29, "rctrl": 97, "lalt": 56, "ralt": 100,
	"minus": 12, "equal": 13, "backslash": 43, "comma": 51, "dot": 52, "slash": 53,
	"semicolon": 39, "apostrophe": 40, "leftbrace": 26, "rightbrace": 27, "grave": 41,
}

type Keymap struct {
	From string `mapstructure:"from"`
	To   string `mapstructure:"to"`
}

type FocusConfig struct {
	WindowTitle string `mapstructure:"window_title"`
}

type AppConfig struct {
	LogLevel string      `mapstructure:"log_level"`
	Focus    FocusConfig `mapstructure:"focus"`
	KeyMaps  []Keymap    `mapstructure:"keymaps"`
}

func LookupKeyCode(name string) (uint16, bool) {
	code, ok := keyCodeRegistry[strings.ToLower(name)]
	return code, ok
}

func (c *AppConfig) Validate() error {
	var errs []error

	if c.LogLevel == "" {
		c.LogLevel = "INFO"
	} else {
		normalizedLevel := strings.ToUpper(c.LogLevel)
		switch normalizedLevel {
		case "DEBUG", "INFO", "WARN", "ERROR":
			c.LogLevel = normalizedLevel
		default:
			errs = append(errs, fmt.Errorf("log_level: invalid value %q; must be one of DEBUG, INFO, WARN, ERROR", c.LogLevel))
		}
	}

	if len(c.KeyMaps) == 0 {
		errs = append(errs, errors.New("keymaps: at least one keymap entry is required"))
	}

	for i, keyMap := range c.KeyMaps {
		if keyMap.From == "" {
			errs = append(errs, fmt.Errorf("keymaps[%d].from: must not be empty", i))
		} else if _, ok := LookupKeyCode(keyMap.From); !ok {
			errs = append(errs, fmt.Errorf("keymaps[%d].from: unknown key name %q", i, keyMap.From))
		}

		if keyMap.To == "" {
			errs = append(errs, fmt.Errorf("keymaps[%d].to: must not be empty", i))
		} else if _, ok := LookupKeyCode(keyMap.To); !ok {
			errs = append(errs, fmt.Errorf("keymaps[%d].to: unknown key name %q", i, keyMap.To))
		}

		if keyMap.From != "" && keyMap.To != "" && strings.EqualFold(keyMap.From, keyMap.To) {
			errs = append(errs, fmt.Errorf("keymaps[%d]: from and to must differ", i))
		}
	}

	if len(errs) == 0 {
		return nil
	}

	return errors.Join(errs...)
}
