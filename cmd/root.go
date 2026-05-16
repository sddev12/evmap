/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"errors"
	"evmap/internal/config"
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "evmap",
	Short: "Linux key remapper for gamers",
	Long:  `evmap intercepts keyboard input at the OS level and remaps configured keys to new keys. Remapping is only active when the target window is in focus.`,

	Run: handleRoot,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	// Set up flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.evmap.yaml)")

	// Get home dir
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Unable to get users home directory with error: %s", err.Error())
	}

	// Set up Viper
	viper.SetConfigName(".evmap")

	viper.AddConfigPath(".")
	viper.AddConfigPath(homeDir)
	viper.AddConfigPath(cfgFile)

	var fileLookupError viper.ConfigFileNotFoundError
	if err := viper.ReadInConfig(); err != nil {
		if errors.As(err, &fileLookupError) {
			log.Fatal("Unable to find config file. Ensure it is in your home directory and has the name .evmap.yaml")
		} else {
			log.Fatal(err)
		}
	}
}

func handleRoot(cmd *cobra.Command, args []string) {
	if err := viper.Unmarshal(&config.Config); err != nil {
		log.Fatalf("Unable to decode config: %s", err)
	}

	for i, keyMap := range config.Config.KeyMaps {
		fmt.Println("Keymap", i)
		fmt.Printf("From: %s To: %s\n", keyMap.From, keyMap.To)
	}
}
