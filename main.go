package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/jhillyerd/enmime"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
)

// Retrieve a token, saves the token, then returns the generated client.
func getClient(config *oauth2.Config) *http.Client {
	// The file token.json stores the user's access and refresh tokens, and is
	// created automatically when the authorization flow completes for the first
	// time.
	tokFile := "token.json"
	tok, err := tokenFromFile(tokFile)
	if err != nil {
		tok = getTokenFromWeb(config)
		saveToken(tokFile, tok)
	}
	return config.Client(context.Background(), tok)
}

// Request a token from the web, then returns the retrieved token.
func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the "+
		"authorization code: \n%v\n", authURL)

	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		log.Fatalf("Unable to read authorization code: %v", err)
	}

	tok, err := config.Exchange(context.TODO(), authCode)
	if err != nil {
		log.Fatalf("Unable to retrieve token from web: %v", err)
	}
	return tok
}

// Retrieves a token from a local file.
func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

// Saves a token to a file path.
func saveToken(path string, token *oauth2.Token) {
	fmt.Printf("Saving credential file to: %s\n", path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

func main() {
	b, err := ioutil.ReadFile("credentials.json") // Download own credentials.json from google developer console.
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}

	// If modifying these scopes, delete your previously saved token.json.
	config, err := google.ConfigFromJSON(b, gmail.GmailReadonlyScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}
	client := getClient(config)

	srv, err := gmail.New(client)
	if err != nil {
		log.Fatalf("Unable to retrieve Gmail client: %v", err)
	}

	user := "me"
	r, err := srv.Users.Labels.List(user).Do()
	if err != nil {
		log.Fatalf("Unable to retrieve labels: %v", err)
	}
	if len(r.Labels) == 0 {
		fmt.Println("No labels found.")
		return
	}

	socialLabelID := ""
	for _, l := range r.Labels {
		if l.Name != "CATEGORY_SOCIAL" {
			continue
		}
		socialLabelID = l.Id
	}
	if socialLabelID == "" {
		fmt.Println("CATEGORY_SOCIAL can not found.")
		return
	}

	for {
		messages, _ := srv.Users.Messages.List("me").LabelIds(socialLabelID).Do()
		for _, m := range messages.Messages {
			mm, err := srv.Users.Messages.Get("me", m.Id).Format("raw").Do()
			if err != nil {
				fmt.Println(err)
				break
			}

			// アーカイブが最大12時間だから、開始時は余裕もって13時間前までのメールをチェックする
			fmt.Println(time.Unix(mm.InternalDate/1000, 0))

			html, err := getLiveStreamHTML(mm.Raw)
			if err != nil {
				fmt.Println(err)
				continue
			}

			stringReader := strings.NewReader(html)
			doc, err := goquery.NewDocumentFromReader(stringReader)
			if err != nil {
				fmt.Print("scarapping failed")
				continue
			}

			liveURL := ""
			sss := doc.Find("a")
			sss.EachWithBreak(func(_ int, s *goquery.Selection) bool {
				url, exists := s.Attr("href")
				if !exists || !strings.Contains(url, "watch") {
					return true
				}

				liveURL = url
				return false
			})

			vid, err := getVideoID(liveURL)
			if err != nil {
				fmt.Println(err)
				continue
			}
			fmt.Println(vid)
		}
	}
}

func getVideoIDsFromList(srv *gmail.Service, socialLabelID string) (vid []string, history string, err error) {
	messages, _ := srv.Users.Messages.List("me").LabelIds(socialLabelID).Do()
	for _, m := range messages.Messages {
		mm, err := srv.Users.Messages.Get("me", m.Id).Format("raw").Do()
		if err != nil {
			fmt.Println(err)
			break
		}

		// アーカイブが最大12時間だから、開始時は余裕もって13時間前までのメールをチェックする
		fmt.Println(time.Unix(mm.InternalDate/1000, 0))

		html, err := getLiveStreamHTML(mm.Raw)
		if err != nil {
			fmt.Println(err)
			continue
		}

		stringReader := strings.NewReader(html)
		doc, err := goquery.NewDocumentFromReader(stringReader)
		if err != nil {
			fmt.Print("scarapping failed")
			continue
		}

		liveURL := ""
		sss := doc.Find("a")
		sss.EachWithBreak(func(_ int, s *goquery.Selection) bool {
			url, exists := s.Attr("href")
			if !exists || !strings.Contains(url, "watch") {
				return true
			}

			liveURL = url
			return false
		})

		vid, err := getVideoID(liveURL)
		if err != nil {
			fmt.Println(err)
			continue
		}
		fmt.Println(vid)
	}
}

func getLiveStreamHTML(src string) (string, error) {
	decoded, err := base64.URLEncoding.DecodeString(src)
	if err != nil {
		fmt.Println(err)
		return "", err
	}

	enve, err := enmime.ReadEnvelope(strings.NewReader(string(decoded)))
	if err != nil {
		fmt.Println(err)
		return "", err
	}

	header := enve.GetHeader("Subject")
	fmt.Println(header)
	if !strings.Contains(header, "ライブ配信中です") {
		return "", fmt.Errorf("not live stream mail")
	}

	return enve.HTML, nil
}

func getVideoID(liveURL string) (string, error) {
	u, err := url.Parse(liveURL)
	if err != nil {
		return "", err
	}
	fmt.Println(u)
	fmt.Println(u.Query().Get("u"))

	u, err = url.Parse(u.Query().Get("u"))
	if err != nil {
		return "", err
	}

	return u.Query().Get("v"), nil
}
