package beater

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
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
			event := common.MapStr{
				"@timestamp": common.Time(time.Now()),
				"type":       "wordbeat",
				"modtime":    common.Time(t),
				"filename":   f.Name(),
				"text":       getText(path),
				//"path":       path,
				//"directory":  f.IsDir(),
				//"filesize":   f.Size(),
			}
			bt.client.PublishEvent(event)
		}

		if f.IsDir() {
			bt.listDir(path)
		}
	}
}

func getText(path string) string {
	//unzip -p document.docx word/document.xml | sed -e 's/<\/w:p>/\n/g; s/<[^>]\{1,\}>//g; s/[^[:print:]\n]\{1,\}//g'

	unzipArgs := []string{"-p", path, "word/document.xml"}

	sedCmd := "s/<\\/w:p>/\\n/g; s/<[^>]\\{1,\\}>//g; s/[^[:print:]\\n]\\{1,\\}//g"

	unzipOut, err := exec.Command("unzip", unzipArgs...).Output()
	if err != nil {
		panic(err)
	}

	tmpfile, err := ioutil.TempFile("", "wordbeat")
	if err != nil {
		log.Fatal(err)
	}
	defer os.Remove(tmpfile.Name()) // clean up

	if _, err := tmpfile.Write(unzipOut); err != nil {
		log.Fatal(err)
	}
	if err := tmpfile.Close(); err != nil {
		log.Fatal(err)
	}

	sedArgs := []string{"-e", sedCmd, tmpfile.Name()}
	fmt.Println(sedArgs)
	sedOut, err := exec.Command("sed", sedArgs...).Output()
	if err != nil {
		fmt.Println(sedOut)
		fmt.Println(err)
		panic(err)
	}

	return string(sedOut)
}
