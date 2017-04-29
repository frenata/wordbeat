package beater

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os/exec"
	"path/filepath"
	"strconv"
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

			filename := strings.TrimPrefix(path, bt.config.Path)
			fulltext := strings.ToLower(extractText(path))

			plans := strings.Split(fulltext, "daily lesson plan")
			for i := 1; i < len(plans); i++ {
				if i > 1 {
					filename = filename + strconv.Itoa(i)
				}
				event, err := parseLessonPlan(plans[i], filename, t)
				if err == nil {
					bt.client.PublishEvent(event)
				}
			}

		}
		if f.IsDir() {
			bt.listDir(path)
		}
	}
}

func parseLessonPlan(fulltext, filename string, modTime time.Time) (common.MapStr, error) {
	lines := strings.Split(fulltext, "\n")
	/*if len(lines) < 5 || !isDailyPlan(lines[:5]) {
		return nil, errors.New("not a daily plan")
	}*/

	teachers := extractTeacher(lines)
	eslrs := extractESLR(lines)

	event := common.MapStr{
		"@timestamp": common.Time(time.Now()),
		"type":       "wordbeat",
		"modtime":    common.Time(modTime),
		"filename":   filename,
		"fulltext":   fulltext,
		"eslr":       eslrs,
		"eslr_num":   len(eslrs),
		"teacher":    teachers,
	}
	return event, nil
}

func isDailyPlan(lines []string) bool {
	for _, line := range lines {
		if strings.Contains(line, "daily lesson plan") {
			return true
		}
	}
	return false
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

	return buff.String()
}

func extractESLR(lines []string) []string {
	eslrs := make([]string, 0)

	capture := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "eslrs") || strings.HasPrefix(line, "eslr’s") {
			line = strings.TrimPrefix(line, "eslrs")
			line = strings.TrimPrefix(line, "eslr’s") // tom!

			sep := ";"
			if !strings.Contains(line, ";") && strings.Contains(line, ".") {
				sep = "."
			}

			e := strings.Split(line, sep)
			for _, l := range e {
				if cleanESLR(l) != "" {
					eslrs = append(eslrs, cleanESLR(l))
				}
			}
			capture = true
		} else if strings.HasPrefix(line, "biblical integration") {
			break
		} else if capture && cleanESLR(line) != "" {
			eslrs = append(eslrs, cleanESLR(line))
		}
	}

	return eslrs
}

func cleanESLR(eslr string) string {
	clean := strings.Trim(eslr, "0123456789.,;: ")
	clean = strings.TrimSpace(clean)
	clean = strings.TrimSuffix(clean, "(all)")
	clean = strings.TrimSpace(clean)
	clean = strings.TrimSuffix(clean, "who")
	clean = strings.TrimSpace(clean)
	clean = strings.Replace(clean, "jesus christ", "christ", 1) // saskia!
	return clean
}

func extractTeacher(lines []string) []string {
	values := make([]string, 0)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "teacher/year level/course:") {
			teacherline := strings.Split(strings.TrimPrefix(line, "teacher/year level/course:"), "/")[0]

			sep := "&amp;"
			if !strings.Contains(teacherline, sep) {
				sep = " and "
			}

			teachers := strings.Split(strings.TrimSpace(teacherline), sep)
			fmt.Println(teachers, len(teachers))
			for _, teacher := range teachers {
				clean := strings.Split(teacher, "-")[0]
				clean = strings.Split(clean, ",")[0]
				values = append(values, strings.TrimSpace(clean))
			}
			return values
		}
	}

	return values
}
