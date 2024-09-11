package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"text/template"
)

//go:embed templates/*
var templateFS embed.FS

const (
	maxScores = 6
)

type InputConfig struct {
	Classes []string       `json:"classes"`
	Objects []InputObjects `json:"objects"`
}

type InputObjects struct {
	FilePath string    `json:"filePath"`
	Class    int       `json:"class"`
	Label    *int      `json:"label"`
	Scores   []float32 `json:"scores"`
}

type TemplateData struct {
	Classes []TemplateClass
	Objects []TemplateObject
}

type TemplateObject struct {
	FilePath   string
	BestScore  TemplateObjectScore
	Label      *string
	Scores     []TemplateObjectScore
	ClassColor string
}

type TemplateObjectScore struct {
	Class string
	Score float32
}

type TemplateClass struct {
	Name  string
	Color string
}

func main() {
	exit(run())
}

func run() error {
	cfg, err := readConfig()
	if err != nil {
		return err
	}

	tmpl, err := template.ParseFS(templateFS, "templates/*.tmpl.html")
	if err != nil {
		return err
	}

	tmplData := mapTemplateData(*cfg)

	addr := "127.0.0.1:2849"
	url := fmt.Sprintf("http://%s/cviz", addr)

	done := make(chan struct{})

	go func() {
		startHttpServer(addr, tmpl, tmplData)
		done <- struct{}{}
	}()

	fmt.Printf("Opening cviz at: %s\n", url)
	openURL(url)

	<-done

	return nil
}

func exit(err error) {
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}
}

func readConfig() (*InputConfig, error) {
	if len(os.Args) < 2 {
		return nil, fmt.Errorf("provide input config path to read")
	}

	file, err := os.OpenFile(os.Args[1], os.O_RDONLY, 0o644)
	if err != nil {
		return nil, err
	}

	var cfg InputConfig
	err = json.NewDecoder(file).Decode(&cfg)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func mapTemplateData(cfg InputConfig) TemplateData {
	colors := generateColors(len(cfg.Classes))
	classes := make([]TemplateClass, 0, len(cfg.Classes))
	for i := range colors {
		classes = append(classes, TemplateClass{
			Name:  cfg.Classes[i],
			Color: colors[i],
		})
	}

	td := TemplateData{
		Classes: classes,
		Objects: make([]TemplateObject, 0, len(cfg.Objects)),
	}
	for _, obj := range cfg.Objects {
		tObj := TemplateObject{
			FilePath: obj.FilePath,
			Scores:   make([]TemplateObjectScore, 0, len(cfg.Classes)),
		}
		if obj.Label != nil {
			label := cfg.Classes[*obj.Label]
			tObj.Label = &label
		}
		bestScoreIdx := 0
		bestScore := TemplateObjectScore{}
		for idx, score := range obj.Scores {
			if idx >= maxScores {
				break
			}
			tScore := TemplateObjectScore{
				Class: cfg.Classes[idx],
				Score: score * 100,
			}
			tObj.Scores = append(tObj.Scores, tScore)
			if tScore.Score > bestScore.Score {
				bestScoreIdx = idx
				bestScore = tScore
			}
		}
		tObj.ClassColor = colors[bestScoreIdx]
		sort.Slice(tObj.Scores, func(i, j int) bool {
			return tObj.Scores[i].Score > tObj.Scores[j].Score
		})
		tObj.Scores = tObj.Scores[1:]
		tObj.BestScore = bestScore
		td.Objects = append(td.Objects, tObj)
	}
	return td
}

func openURL(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start"}
	case "darwin":
		cmd = "open"
	default: // "linux", "freebsd", "openbsd", "netbsd"
		cmd = "xdg-open"
	}
	args = append(args, url)
	return exec.Command(cmd, args...).Start()
}

func startHttpServer(addr string, tmpl *template.Template, tmplData TemplateData) {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		file, err := os.OpenFile(r.URL.Path, os.O_RDONLY, 0o644)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}
		_, err = io.Copy(w, file)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}
	})

	http.HandleFunc("/cviz", func(w http.ResponseWriter, r *http.Request) {
		tmpl.Execute(w, tmplData)
	})

	http.ListenAndServe(addr, nil)
}

func generateColors(nClasses int) []string {
	colors := make([]*Color, 0, nClasses)
	for i := 0; i < nClasses; i++ {
		color := generateColor()
		colors = append(colors, color)
	}
	colorsHex := make([]string, 0, len(colors))
	for _, color := range colors {
		colorsHex = append(colorsHex, color.Hex())
	}
	return colorsHex
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

type Color struct {
	R, G, B int
}

func (c *Color) Hex() string {
	return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B)
}

func (c *Color) Sum() int {
	return c.R + c.G + c.B
}

func generateBrightColor() *Color {
	const brightThreshold = 350 // ensure that colors are bright
	color := generateColor()
	for color.Sum() < brightThreshold {
		color = generateColor()
	}
	return color
}

func generateColor() *Color {
	const min = 64
	const brightThreshold = 128

	var r, g, b int

	r = rand.Intn(256)
	if r > brightThreshold {
		g = rand.Intn(256-min) + min
		b = rand.Intn(256-min) + min
	} else {
		g = rand.Intn(256-brightThreshold) + brightThreshold
		b = rand.Intn(256-brightThreshold) + brightThreshold
	}

	return &Color{
		R: r,
		G: g,
		B: b,
	}
}
