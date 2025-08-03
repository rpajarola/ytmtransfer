package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

// Config holds OAuth2 configurations for both accounts
type Config struct {
	SourceConfig oauth2.Config
	TargetConfig oauth2.Config
}

func main() {
	ctx := context.Background()

	log.Print("Reading credentials from credentials.json")
	// Load client credentials
	b, err := os.ReadFile("credentials.json")
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}

	log.Print("Configuring OAuth2")
	// Configure OAuth2 for both accounts
	config, err := google.ConfigFromJSON(b, youtube.YoutubeReadonlyScope, youtube.YoutubeScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}

	log.Print("getting source_token.json")
	// Get tokens for both accounts
	sourceClient := getClient(config, "source_token.json")
	log.Print("getting target_token.json")
	targetClient := getClient(config, "target_token.json")

	// Create YouTube service instances
	log.Print("creating source youtube service")
	sourceService, err := youtube.NewService(ctx, option.WithHTTPClient(sourceClient))
	if err != nil {
		log.Fatalf("Error creating source YouTube service: %v", err)
	}

	log.Print("creating target youtube service")
	targetService, err := youtube.NewService(ctx, option.WithHTTPClient(targetClient))
	if err != nil {
		log.Fatalf("Error creating target YouTube service: %v", err)
	}

	log.Print("Transfering Likes")

	// Transfer likes
	if err := transferLikes(sourceService, targetService); err != nil {
		log.Fatalf("Error transferring likes: %v", err)
	}
}

func getClient(config *oauth2.Config, tokFile string) *http.Client {
	tok, err := tokenFromFile(tokFile)
	if err != nil {
		tok = getTokenFromWeb(config)
		saveToken(tokFile, tok)
	}
	return config.Client(context.Background(), tok)
}

func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	codeCh := make(chan string)

	// Start local server
	server := &http.Server{Addr: ":8080"}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code != "" {
			fmt.Fprintf(w, "Authorization successful! You can close this window.")
			codeCh <- code
		}
	})

	go server.ListenAndServe()
	defer server.Shutdown(context.Background())

	config.RedirectURL = "http://localhost:8080"
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Opening browser for authorization: %v\n", authURL)

	// Wait for code
	authCode := <-codeCh

	tok, err := config.Exchange(context.TODO(), authCode)
	if err != nil {
		log.Fatalf("Unable to retrieve token: %v", err)
	}
	return tok
}

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

func saveToken(path string, token *oauth2.Token) {
	fmt.Printf("Saving credential file to: %s\n", path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

func transferLikes(source, target *youtube.Service) error {
	pageToken := ""
	likedVideos := []string{}

	// Fetch all liked videos from source account
	for {
		call := source.Videos.List([]string{"id"}).
			MyRating("like").
			MaxResults(50)

		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		response, err := call.Do()
		if err != nil {
			return fmt.Errorf("error fetching liked videos: %v", err)
		}

		for _, item := range response.Items {
			likedVideos = append(likedVideos, item.Id)
		}

		pageToken = response.NextPageToken
		if pageToken == "" {
			break
		}
	}

	fmt.Printf("Found %d liked videos\n", len(likedVideos))

	// Like videos on target account
	for i, videoId := range likedVideos {
		err := target.Videos.Rate(videoId, "like").Do()
		if err != nil {
			fmt.Printf("Error liking video %s: %v\n", videoId, err)
			continue
		}
		log.Printf("Liked video %d/%d: %s\n", i+1, len(likedVideos), videoId)
	}

	return nil
}
