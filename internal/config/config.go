package config

var Config AppConfig

type Keymap struct {
	From string `mapstructure:"from"`
	To   string `mapstructure:"to"`
}

type AppConfig struct {
	LogLevel string   `mapstructure:"log_level"`
	KeyMaps  []Keymap `mapstructure:"keymaps"`
}
