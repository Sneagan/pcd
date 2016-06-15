/*
	pcd - Simple, lightweight podcatcher in golang
	Copyright (C) 2016  Kristof Vannotten

	This program is free software: you can redistribute it and/or modify
	it under the terms of the GNU General Public License as published by
	the Free Software Foundation, either version 3 of the License, or
	(at your option) any later version.

	This program is distributed in the hope that it will be useful,
	but WITHOUT ANY WARRANTY; without even the implied warranty of
	MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
	GNU General Public License for more details.

	You should have received a copy of the GNU General Public License
	along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/
package feedparser

import (
	"bufio"
	"encoding/gob"
	"encoding/xml"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cheggaaa/pb"
	"github.com/kvannotten/pcd/configuration"
)

type PodcastFeed struct {
	XMLName xml.Name `xml:"rss"`
	Channel Channel
}

type Channel struct {
	XMLName     xml.Name `xml:"channel"`
	Items       []Item   `xml:"item"`
	Title       ChannelTitle
	Description ChannelDescription
}

type ChannelTitle struct {
	XMLName xml.Name `xml:"title"`
	Title   string   `xml:",chardata"`
}

type ChannelDescription struct {
	XMLName     xml.Name `xml:"description"`
	Description string   `xml:",chardata"`
}

type Item struct {
	Title      ItemTitle
	Enclosure  Enclosure
	Downloaded bool
	Date       PodcastDate
}

type ItemTitle struct {
	XMLName xml.Name `xml:"title"`
	Title   string   `xml:",chardata"`
}

type ItemLink struct {
	XMLName xml.Name `xml:"link"`
	Link    string   `xml:",chardata"`
}

type Enclosure struct {
	XMLName xml.Name `xml:"enclosure"`
	URL     string   `xml:"url,attr"`
	Length  int      `xml:"length,attr"`
	Type    string   `xml:"type,attr"`
}

type PodcastDate struct {
	XMLName xml.Name `xml:"pubDate"`
	Date    string   `xml:",chardata"`
}

func Parse(podcast configuration.Podcast, wg *sync.WaitGroup, throttle chan int) {
	defer wg.Done()
	resp, err := doRequest(podcast.Feed, podcast.Username, podcast.Password)
	if err != nil {
		fmt.Printf("Could not fetch feed: %s due to:\t %s\n", podcast.Name, err)
		return
	}
	defer resp.Body.Close()

	var feed PodcastFeed
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Unable to read response: %s\n", err)
		return
	}

	if err := xml.Unmarshal(body, &feed); err != nil {
		fmt.Printf("Response is not a valid podcast feed: %s\n", err)
		return
	}

	cachedFeed := readCachedFeed(podcast)
	sortFeedByDate(feed)
	sortFeedByDate(cachedFeed)
	if len(cachedFeed.Channel.Items) < 1 {
		// NOOP
	} else if len(feed.Channel.Items) < 1 {
		fmt.Println("This podcast has no items")
		return
	} else {
		if cachedFeed.Channel.Items[0].Title == feed.Channel.Items[0].Title {
			// NOOP
		} else {
			fmt.Printf("New items for podcast %s\n", feed.Channel.Title.Title)
		}
	}

	writeFeed(podcast, feed)
	<-throttle
}

func Download(podcast configuration.Podcast, number int) {
	feed := readCachedFeed(podcast)
	url := feed.Channel.Items[number-1].Enclosure.URL

	resp, err := doRequest(url, podcast.Username, podcast.Password)
	if err != nil {
		fmt.Printf("Could not download podcast: %s\n", err)
		return
	}
	defer resp.Body.Close()

	tokens := strings.Split(url, "/")
	filename := tokens[len(tokens)-1]

	writePodcast(podcast, resp.Body, filename, feed.Channel.Items[number-1].Enclosure.Length)
}

func GetFileNameForPodcastAndEpisode(podcast configuration.Podcast, number int) string {
	feed := readCachedFeed(podcast)
	url := feed.Channel.Items[number-1].Enclosure.URL
	tokens := strings.Split(url, "/")
	filename := tokens[len(tokens)-1]

	return filename
}

func ListEpisodes(podcast configuration.Podcast) []Item {
	items := make([]Item, 0)
	feed := readCachedFeed(podcast)

	for i := 0; i < len(feed.Channel.Items); i++ {
		item := feed.Channel.Items[i]

		tokens := strings.Split(item.Enclosure.URL, "/")
		filename := tokens[len(tokens)-1]
		path := filepath.Join(podcast.Path, filename)

		if _, err := os.Stat(path); os.IsNotExist(err) {
			item.Downloaded = false
		} else {
			item.Downloaded = true
		}
		items = append(items, item)
	}

	return items
}

func doRequest(url, username, password string) (*http.Response, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)

	if err != nil {
		return nil, err
	}

	if len(username) > 0 && len(password) > 0 {
		req.SetBasicAuth(username, password)
	}

	return client.Do(req)
}

func writePodcast(podcast configuration.Podcast, reader io.Reader, filename string, lengthInBytes int) {
	path := filepath.Join(podcast.Path, filename)
	fmt.Printf("Downloading podcast to %s\n", path)

	f, err := os.Create(path)
	if err != nil {
		panic("Could not create file")
	}
	defer f.Close()

	bar := pb.New(lengthInBytes).SetUnits(pb.U_BYTES)
	bar.ShowSpeed = true
	bar.Start()

	mw := io.MultiWriter(f, bar)
	_, err = io.Copy(mw, reader)
	if err != nil {
		panic("Could not download file")
	}
	bar.Finish()
}

func writeFeed(podcast configuration.Podcast, feed PodcastFeed) {
	err := os.MkdirAll(podcast.Path, 0700)
	path := filepath.Join(podcast.Path, ".cache")
	f, err := os.Create(path)
	if err != nil {

	}
	defer f.Close()
	w := bufio.NewWriter(f)
	enc := gob.NewEncoder(w)
	enc.Encode(feed)

	w.Flush()
}

func readCachedFeed(podcast configuration.Podcast) PodcastFeed {
	path := filepath.Join(podcast.Path, ".cache")
	f, err := os.Open(path)
	if err != nil {
		return PodcastFeed{}
	}
	defer f.Close()
	r := bufio.NewReader(f)
	dec := gob.NewDecoder(r)
	var feed PodcastFeed
	dec.Decode(&feed)

	return feed
}

func sortFeedByDate(feed PodcastFeed) []Item {
	layout := "Mon, 02 Jan 2006 15:04:05 -0700"
	firstDate, _ := time.Parse(layout, feed.Channel.Items[0].Date.Date)
	lastDate, _ := time.Parse(layout, feed.Channel.Items[len(feed.Channel.Items)-1].Date.Date)

	if firstDate.Before(lastDate) {
		// reverse feed
		for i, j := 0, len(feed.Channel.Items)-1; i < j; i, j = i+1, j-1 {
			feed.Channel.Items[i], feed.Channel.Items[j] = feed.Channel.Items[j], feed.Channel.Items[i]
		}
	}

	return feed.Channel.Items
}
