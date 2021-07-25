package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/mattermost/mattermost-server/v5/model"
	"github.com/mattermost/mattermost-server/v5/plugin"
)

// this regexp matches links to Hacker News pages with or without scheme
var hnURLPattern = regexp.MustCompile(`((http|https):\/\/)?news\.ycombinator\.com\/item\?id=[0-9]+`)

// Plugin implements the interface expected by the Mattermost server to communicate between the server and plugin processes.
type Plugin struct {
	plugin.MattermostPlugin

	// configurationLock synchronizes access to the configuration.
	configurationLock sync.RWMutex

	// configuration is the active plugin configuration. Consult getConfiguration and
	// setConfiguration for usage.
	configuration *configuration

	botId        string
	profileImage []byte
}

func (p *Plugin) MessageWillBePosted(c *plugin.Context, post *model.Post) (*model.Post, string) {
	if post.UserId == p.botId {
		return nil, ""
	}
	if hnURLPattern == nil {
		p.API.LogError("hnurlpattern was nil")
	}

	links := hnURLPattern.FindAllString(post.Message, -1)
	if len(links) < 1 {
		return nil, ""
	}

	for _, link := range links {
		split := strings.SplitAfter(link, `=`)
		if len(split) < 2 {
			continue
		}
		item := split[len(split)-1]

		resp, err := http.Get(fmt.Sprintf(
			"https://hacker-news.firebaseio.com/v0/item/%s.json",
			item))

		if err != nil {
			p.API.LogError(err.Error())
			return nil, ""
		}

		hnresponse, err := p.messageContentFromResponse(resp)
		if err != nil {
			p.API.LogError(err.Error())
			continue
		}
		if hnresponse.Type != "story" {
			continue
		}
		attachments := post.Attachments()
		attachments = append(attachments,
			&model.SlackAttachment{
				AuthorIcon: fmt.Sprintf("/plugins/%s/hn.png", manifest.Id),
				AuthorName: hnresponse.Title,
				AuthorLink: hnresponse.URL,
			})
		post.DelProp("attachments")
		post.AddProp("attachments", attachments)
		return post, ""

		// _, appErr := p.API.CreatePost(
		// 	&model.Post{
		// 		UserId:    p.botId,
		// 		ChannelId: post.ChannelId,
		// 		ParentId:  post.ParentId,
		// 		Message:   fmt.Sprintf("[%s](%s)", hnresponse.Title, hnresponse.URL),
		// 		Metadata: &model.PostMetadata{
		// 			Embeds: []*model.PostEmbed{
		// 				{
		// 					Type: model.POST_EMBED_OPENGRAPH,
		// 					URL:  hnresponse.URL,
		// 					Data: map[string]string{
		// 						"title": hnresponse.Title,
		// 						"url":   hnresponse.URL,
		// 					},
		// 				},
		// 			},
		// 		},
		// 	},
		// )
	}
	return nil, ""
}

type HNResponse struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

func (p *Plugin) messageContentFromResponse(resp *http.Response) (*HNResponse, error) {
	response, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if response == nil {
		return nil, errors.New("response from upstream was nil")
	}

	var w HNResponse
	if err := json.Unmarshal(response, &w); err != nil {
		return nil, err
	}

	return &w, nil
}

func (p *Plugin) OnActivate() (err error) {
	p.botId, err = p.Helpers.EnsureBot(&model.Bot{
		Username:    "hackernewsbot",
		DisplayName: "Hacker News Bot",
		Description: "The Hacker News Bot",
	})

	if err != nil {
		return err
	}

	bundlePath, err := p.API.GetBundlePath()
	if err != nil {
		return err
	}

	p.profileImage, err = ioutil.ReadFile(filepath.Join(bundlePath, "assets", "hn.png"))
	if err != nil {
		p.API.LogError(fmt.Sprintf(err.Error(), "couldn't read profile image: %s"))
		return err
	}

	appErr := p.API.SetProfileImage(p.botId, p.profileImage)
	if appErr != nil {
		return appErr
	}

	return nil
}

func (p *Plugin) ServeHTTP(c *plugin.Context, w http.ResponseWriter, r *http.Request) {
	if p == nil {
		return
	}
	if p.profileImage == nil {
		p.API.LogError("profile image was nil")
		return
	}

	w.Write(p.profileImage)
}
