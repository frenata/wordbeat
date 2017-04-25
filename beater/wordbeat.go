package beater

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/elastic/beats/libbeat/beat"
	"github.com/elastic/beats/libbeat/common"
	"github.com/elastic/beats/libbeat/logp"
	"github.com/elastic/beats/libbeat/publisher"

	"github.com/frenata/wordbeat/config"
)

type Wordbeat struct {
	done          chan struct{}
	config        config.Config
	client        publisher.Client
	lastIndexTime time.Time
}

// Creates beater
func New(b *beat.Beat, cfg *common.Config) (beat.Beater, error) {
	config := config.DefaultConfig
	if err := cfg.Unpack(&config); err != nil {
		return nil, fmt.Errorf("Error reading config file: %v", err)
	}

	bt := &Wordbeat{
		done:   make(chan struct{}),
		config: config,
	}
	return bt, nil
}

func (bt *Wordbeat) Run(b *beat.Beat) error {
	logp.Info("wordbeat is running! Hit CTRL-C to stop it.")

	bt.client = b.Publisher.Connect()
	ticker := time.NewTicker(bt.config.Period)
	if !bt.config.ScanAll {
		bt.lastIndexTime = time.Now()
	}

	for {
		now := time.Now()
		bt.listDir(bt.config.Path)
		bt.lastIndexTime = now

		logp.Info("Event sent")
		select {
		case <-bt.done:
			return nil
		case <-ticker.C:
		}
	}
}

func (bt *Wordbeat) Stop() {
	bt.client.Close()
	close(bt.done)
}

func (bt *Wordbeat) listDir(dirFile string) {
	files, _ := ioutil.ReadDir(dirFile)
	for _, f := range files {
		t := f.ModTime()
		path := filepath.Join(dirFile, f.Name())

		if !f.IsDir() &&
			strings.HasSuffix(path, ".docx") &&
			t.After(bt.lastIndexTime) {

			fulltext := extractText(path)
			lines := strings.Split(fulltext, "\n")

			event := common.MapStr{
				"@timestamp":           common.Time(time.Now()),
				"type":                 "wordbeat",
				"modtime":              common.Time(t),
				"filename":             f.Name(),
				"fulltext":             fulltext,
				"eslr":                 extractESLR(lines),
				"essential_questions":  extractEssentialQuestions(lines),
				"teacher":              extractTeacher(lines),
				"biblical_integration": extractBiblicalIntegration(lines),
				"unit_objectives":      extractUnitObjectives(lines),
				"lesson_objectives":    extractLessonObjectives(lines),
			}
			bt.client.PublishEvent(event)
		}

		if f.IsDir() {
			bt.listDir(path)
		}
	}
}

func extractText(path string) string {
	//unzip -p document.docx word/document.xml | sed -e 's/<\/w:p>/\n/g; s/<[^>]\{1,\}>//g; s/[^[:print:]\n]\{1,\}//g'

	unzipArgs := []string{"-p", path, "word/document.xml"}
	unzip := exec.Command("unzip", unzipArgs...)

	sedCmd := "s/<\\/w:p>/\\n/g; s/<[^>]\\{1,\\}>//g; s/[^[:print:]\\n]\\{1,\\}//g"
	sedArgs := []string{"-e", sedCmd}
	sed := exec.Command("sed", sedArgs...)

	var buff bytes.Buffer
	var err error
	sed.Stdin, err = unzip.StdoutPipe()
	sed.Stdout = &buff
	err = sed.Start()
	err = unzip.Run()
	err = sed.Wait()
	if err != nil {
		panic(err)
	}

	//fmt.Println(buff)
	return buff.String()
}

func extractESLR(lines []string) []string {
	eslrs := make([]string, 0)

	capture := false
	for _, line := range lines {
		if line == "ESLRs:" {
			capture = true
		} else if line == "Biblical Integration:" {
			break
		} else if capture && line != "" {
			clean := strings.TrimLeft(line, "0123456789. ")
			eslrs = append(eslrs, clean)
		}
	}

	return eslrs
}

func extractEssentialQuestions(lines []string) []string {
	questions := make([]string, 0)

	capture := false
	for _, line := range lines {
		if line == "Essential Questions:" {
			capture = true
		} else if line == "ESLRs:" {
			break
		} else if capture && line != "" {
			questions = append(questions, line)
		}
	}

	return questions
}

func extractTeacher(lines []string) string {
	for _, line := range lines {
		if strings.HasPrefix(line, "Teacher/Year level/Course:") {
			return strings.Split(strings.TrimPrefix(line, "Teacher/Year level/Course:"), "/")[0]
		}
	}
	return ""
}

func extractBiblicalIntegration(lines []string) []string {
	values := make([]string, 0)

	capture := false
	for _, line := range lines {
		if line == "Biblical Integration:" {
			capture = true
		} else if strings.HasPrefix(line, "Unit Objectives") {
			break
		} else if capture && line != "" {
			values = append(values, line)
		}
	}

	return values
}

func extractUnitObjectives(lines []string) []string {
	values := make([]string, 0)

	capture := false
	for _, line := range lines {
		if strings.HasPrefix(line, "Unit Objectives") {
			capture = true
		} else if strings.HasPrefix(line, "Lesson Objectives") {
			break
		} else if capture && line != "" {
			clean := strings.TrimLeft(line, "0123456789. ")
			values = append(values, clean)
		}
	}

	return values
}

func extractLessonObjectives(lines []string) []string {
	values := make([]string, 0)

	capture := false
	for _, line := range lines {
		if strings.HasPrefix(line, "Lesson Objectives") {
			capture = true
		} else if strings.HasPrefix(line, "Time allocated") {
			break
		} else if capture && line != "" {
			values = append(values, line)
		}
	}

	return values
}
