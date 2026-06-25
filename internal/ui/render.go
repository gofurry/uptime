package ui

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"html/template"
	"io"
)

//go:embed page.html
var pageHTML string

//go:embed style.css
var styleCSS string

//go:embed app.js
var appJS string

var pageTemplate = template.Must(template.New("uptime").Parse(pageHTML))

type Page struct {
	Title           string
	Description     string
	Footer          string
	DefaultTheme    string
	DefaultLanguage string
	Background      string
	Config          ClientConfig
	Status          any
}

type ClientConfig struct {
	DefaultLanguage string `json:"defaultLanguage"`
	DefaultTheme    string `json:"defaultTheme"`
	RefreshMS       int64  `json:"refreshMS"`
	APIPath         string `json:"apiPath"`
}

type pageData struct {
	Title           string
	Description     string
	Footer          string
	DefaultTheme    string
	DefaultLanguage string
	Background      string
	CSS             template.CSS
	JS              template.JS
	ConfigJSON      template.JS
	StatusJSON      template.JS
}

func Render(w io.Writer, page Page) error {
	configJSON, err := json.Marshal(page.Config)
	if err != nil {
		return err
	}
	statusJSON, err := json.Marshal(page.Status)
	if err != nil {
		return err
	}
	data := pageData{
		Title:           page.Title,
		Description:     page.Description,
		Footer:          page.Footer,
		DefaultTheme:    page.DefaultTheme,
		DefaultLanguage: page.DefaultLanguage,
		Background:      page.Background,
		CSS:             template.CSS(styleCSS),
		JS:              template.JS(appJS),
		ConfigJSON:      template.JS(configJSON),
		StatusJSON:      template.JS(statusJSON),
	}
	return pageTemplate.Execute(w, data)
}

func HTML(page Page) (string, error) {
	var buf bytes.Buffer
	if err := Render(&buf, page); err != nil {
		return "", err
	}
	return buf.String(), nil
}
