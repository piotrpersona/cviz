package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"text/template"

	"github.com/google/uuid"
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
	Classes  []TemplateClass
	Objects  []TemplateObject
	LastPage bool
}

type TemplateObject struct {
	ID         string
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
	colors := getColors(len(cfg.Classes))
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
			ID:       string(uuid.NewString()[29:]),
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
		page, limit := parseQueryPaging(r.URL.Query())
		totalLen := len(tmplData.Objects)
		offsetStart := (page - 1) * limit
		if offsetStart >= totalLen {
			offsetStart = totalLen - limit
			if offsetStart < 0 {
				offsetStart = 0
			}
		}
		offsetEnd := offsetStart + limit
		if offsetEnd > totalLen {
			offsetEnd = totalLen
		}
		tmpl.Execute(w, TemplateData{
			Classes:  tmplData.Classes,
			Objects:  tmplData.Objects[offsetStart:offsetEnd],
			LastPage: offsetEnd == totalLen,
		})
	})

	http.ListenAndServe(addr, nil)
}

func parseQueryPaging(values url.Values) (int, int) {
	page := 1
	limit := 20
	if limitStr := values.Get("limit"); limitStr != "" {
		limitInt, err := strconv.Atoi(limitStr)
		if err == nil && limitInt > 0 {
			limit = limitInt
		}
	}
	if pageStr := values.Get("page"); pageStr != "" {
		pageInt, err := strconv.Atoi(pageStr)
		if err == nil && pageInt > 0 {
			page = pageInt
		}
	}
	return page, limit
}

func getColors(n int) []string {
	colors := make([]string, n)
	for i := 0; i < n; i++ {
		it := i % len(baseColors)
		color := baseColors[it]
		colors[i] = color
	}
	return colors
}

var baseColors = []string{
	"#eb6f92",
	"#f6c177",
	"#8ad8e8",
	"#ebbcba",
	"#c4a7e7",
	"#29bdab",
	"#3998f5",
	"#37294f",
	"#277da7",
	"#f22020",
	"#991919",
	"#ffcba5",
	"#e68f66",
	"#c56133",
	"#96341c",
	"#632819",
	"#ffc413",
	"#f47a22",
	"#2f2aa0",
	"#b732cc",
	"#772b9d",
	"#f07cab",
	"#d30b94",
	"#edeff3",
	"#c3a5b4",
	"#946aa2",
	"#5d4c86",
}
