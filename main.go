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
	"strconv"
	"strings"
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
	ID    string  `json:"id"`
	File  string  `json:"file"`
	Label *int    `json:"label"`
	Class int     `json:"class"`
	Score float32 `json:"score"`
}

type TemplateData struct {
	Objects  []TemplateObject
	LastPage bool
}

type TemplateObject struct {
	ID                     string
	FilePath               string
	GroundTruth            *GroundTruth
	PredictedClassName     string
	Score                  float32
	PredictedClassColorHex string
}

type GroundTruth struct {
	ClassName     string
	ClassColorHex string
	Match         bool
}

func main() {
	exit(run())
}

func run() error {
	cfg, err := readConfig()
	if err != nil {
		return err
	}

	if err := validateConfig(cfg); err != nil {
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

	filePath := os.Args[1]
	file, err := os.OpenFile(filePath, os.O_RDONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("cannot open file: '%s', err: %w", filePath, err)
	}

	var cfg InputConfig
	err = json.NewDecoder(file).Decode(&cfg)
	if err != nil {
		return nil, fmt.Errorf("provided config is not a valid JSON, err: %w", err)
	}
	return &cfg, nil
}

func validateConfig(cfg *InputConfig) error {
	errorMessages := make([]string, 0)
	nClasses := len(cfg.Classes)
	for _, object := range cfg.Objects {
		if object.Class < 0 {
			errorMessages = append(errorMessages, fmt.Sprintf("object '%s' class cannot be < 0, found: %d", object.File, object.Class))
		}
		if object.Class > nClasses {
			errorMessages = append(errorMessages, fmt.Sprintf("object '%s' class cannot be > %d (total classes length), found: %d", object.File, nClasses, object.Class))
		}
		if object.Label != nil {
			if *object.Label < 0 {
				errorMessages = append(errorMessages, fmt.Sprintf("object '%s' label cannot be < 0, found: %d", object.File, *object.Label))
			}
			if *object.Label > nClasses {
				errorMessages = append(errorMessages, fmt.Sprintf("object '%s' label cannot be > %d (total classes length), found: %d", object.File, nClasses, *object.Label))
			}
		}
		if object.Score < 0 {
			errorMessages = append(errorMessages, fmt.Sprintf("object '%s' score cannot be < 0.0 found: %.2f", object.File, object.Score))
		}
		if object.Score > 1.0 {
			errorMessages = append(errorMessages, fmt.Sprintf("object '%s' score cannot be > 1.0 found: %.2f", object.File, object.Score))
		}
	}
	if len(errorMessages) > 0 {
		return fmt.Errorf("corrupted config: \n%s", strings.Join(errorMessages, "\n"))
	}
	return nil
}

func mapTemplateData(cfg InputConfig) TemplateData {
	td := TemplateData{
		Objects: make([]TemplateObject, 0, len(cfg.Objects)),
	}
	for _, obj := range cfg.Objects {
		objectID := obj.ID
		if objectID == "" {
			splitted := strings.Split(obj.File, "/")
			objectID = splitted[len(splitted)-1]
		}
		tObj := TemplateObject{
			ID:                     objectID,
			FilePath:               obj.File,
			Score:                  obj.Score * 100,
			PredictedClassName:     cfg.Classes[obj.Class],
			PredictedClassColorHex: getColor(obj.Class),
		}
		if obj.Label != nil {
			tObj.GroundTruth = &GroundTruth{
				ClassName:     cfg.Classes[*obj.Label],
				ClassColorHex: getColor(*obj.Label),
				Match:         *obj.Label == obj.Class,
			}
		}
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
		defer file.Close()
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

func getColor(n int) string {
	it := n % len(baseColors)
	return baseColors[it]
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
