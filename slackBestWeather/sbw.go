package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"sort"
)

type loc struct {
	lat, lng float64
}

var (
	locations = map[string]loc{
		"Islip":      loc{lat: 40.726911, lng: -73.218542},
		"Bryn Mawr":  loc{lat: 40.0274743, lng: -75.3118813},
		"Ann Arbor":  loc{lat: 42.288873, lng: -83.74613},
		"Dublin":     loc{lat: 53.3403505, lng: -6.3534707}, // ballyer
		"Greenville": loc{lat: 34.844068, lng: -82.404295},
		"Anna Maria": loc{lat: 27.499887, lng: -82.715927},
	}
)

// Struct to unmarshal json from forcast.io
// Only the stuff I'm interested in atm
type fioResp struct {
	Daily struct {
		Data []struct {
			Humidity          float64
			CloudCover        float64
			PrecipProbability float64
			Pressure          float64
			Summary           string
			TemperatureMax    float64
			TemperatureMin    float64
			Time              float64
			Icon              string
		}
	}
}

type locScore struct {
	Location string
	Score    int
	Summary  string
	Icon     string
}

func main() {
	slackWebhook := flag.String("webhook", "", "Webhook URL for a slack channel")
	useCache := flag.Bool("c", false, "Cache the results from the weather service. (For testing)")
	flag.Parse()
	res := make([]locScore, 0)
	// get weather data from forcast.io
	var f fioResp
	for k, v := range locations {
		d, err := get(v, *useCache)
		if err != nil {
			panic(err)
		}
		err = json.Unmarshal(d, &f)
		if err != nil {
			panic(err)
		}
		n := score(&f)
		res = append(res, locScore{Score: n, Location: k, Summary: f.Daily.Data[0].Summary, Icon: f.Daily.Data[0].Icon})
	}
	sort.Sort(byScore(res))
	sendToSlack(*slackWebhook, res)
}

func sendToSlack(webhook string, res []locScore) error {
	type Field struct {
		Title string `json:"title,omitempty"`
		Value string `json:"value"`
		Short bool   `json:"short,omitempty"`
	}
	type Attachment struct {
		Fallback    string  `json:"fallback,omitempty"`
		Color       string  `json:"color,omitempty"`
		PreText     string  `json:"pretext,omitempty"`
		Author_Name string  `json:"author_name,omitempty"`
		Author_Link string  `json:"author_link,omitempty"`
		Author_icon string  `json:"author_icon,omitempty"`
		Title       string  `json:"title,omitempty"`
		Title_Link  string  `json:"title_link,omitempty"`
		Text        string  `json:"text"`
		Fields      []Field `json:"fields,omitempty"`
		Image_URL   string  `json:"image_url,omitempty"`
		Thumb_URL   string  `json:"thumb_url,omitempty"`
	}

	type slackMsg struct {
		Text        string       `json:"text"`
		Username    string       `json:"username,omitempty"`
		Icon_Emoji  string       `json:"icon_emoji,omitempty"`
		Channel     string       `json:"channel,omitempty"`
		Attachments []Attachment `json:"attachments,omitempty"`
	}
	var sm slackMsg
	sm.Text = "Results of the best weather competition today are:"
	//sm.Channel = "#general"
	maxScore := res[0].Score
	minScore := res[len(res)-1].Score
	for i, v := range res {
		f := []Field{
			{Value: v.Location, Short: true},
			{Value: fmt.Sprintf("%d", v.Score), Short: true},
			{Value: v.Summary},
		}
		if i == 0 {
			f[0].Title = "Location"
			f[1].Title = "Score"
		}
		sm.Attachments = append(sm.Attachments, Attachment{
			Fields:    f,
			Color:     getValueBetweenTwoFixedColors(float64(v.Score-minScore) / float64((maxScore - minScore))),
			Thumb_URL: fmt.Sprintf(":%s:", v.Icon),
		})
	}
	buf, err := json.MarshalIndent(sm, "", " ")
	if err != nil {
		return err
	}
	if webhook == "" {
		fmt.Println(string(buf))
		return nil
	}
	body := bytes.NewBuffer(buf)
	resp, err := http.Post(webhook, "application/json", body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad http response %s", resp.Status)
	}
	return nil
}

type byScore []locScore

func (ls byScore) Len() int {
	return len(ls)
}

func (ls byScore) Less(a, b int) bool {
	return ls[a].Score > ls[b].Score
}

func (ls byScore) Swap(a, b int) {
	ls[a], ls[b] = ls[b], ls[a]
}

const (
	perfectMaxTemp  = 80
	perfectMinTemp  = 60
	perfectHumidity = .6
)

func score(f *fioResp) int {
	today := f.Daily.Data[0]
	tmax := today.TemperatureMax
	if tmax > perfectMaxTemp {
		tmax = perfectMaxTemp*2 - tmax
	}
	tmax += 100 - perfectMaxTemp
	tmin := today.TemperatureMin
	if tmin > perfectMinTemp {
		tmin = perfectMinTemp*2 - tmin
	}
	tmin += 100 - perfectMinTemp
	ccover := int((1.0 - today.CloudCover) * 100)
	precip := int((1.0 - today.PrecipProbability) * 100)
	h := today.Humidity
	if h > perfectHumidity {
		h = perfectHumidity*2 - h
	}
	humid := int(h*100 + 40)
	return (int(tmax*2) + int(tmin) + ccover + precip + humid)
}

func get(l loc, useCache bool) ([]byte, error) {
	u := fmt.Sprintf("https://api.forecast.io/forecast/52d39c0c95e7f6f475e316c6c516b5e7/%f,%f", l.lat, l.lng)
	fn := fmt.Sprintf("cache/%x", sha1.Sum([]byte(u)))
	buf, err := ioutil.ReadFile(fn)
	if useCache && err == nil && len(buf) > 0 {
		return buf, nil
	}
	resp, err := http.Get(u)
	if err != nil {
		return nil, err
	}
	buf, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	ioutil.WriteFile(fn, buf, 0740)
	return buf, nil
}

func getValueBetweenTwoFixedColors(value float64) string {
	aR := 255.0
	aG := 0.0
	aB := 0.0
	bR := 0.0
	bG := 255.0
	bB := 0.0

	red := int((bR-aR)*value + aR)   // Evaluated as -255*value + 255.
	green := int((bG-aG)*value + aG) // Evaluates as 0.
	blue := int((bB-aB)*value + aB)  // Evaluates as 255*value + 0.
	return fmt.Sprintf("#%02x%02x%02x", red, green, blue)
}
