package main

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cristalhq/jwt/v2"
	"github.com/mattermost/mattermost-server/v5/mlog"
	"github.com/mattermost/mattermost-server/v5/model"
	"github.com/mattermost/mattermost-server/v5/plugin"
	"github.com/pkg/errors"
)

const jitsiNameSchemaAsk = "ask"
const jitsiNameSchemaEnglish = "english-titlecase"
const jitsiNameSchemaUUID = "uuid"
const jitsiNameSchemaMattermost = "mattermost"
const configChangeEvent = "config_update"

type UserConfig struct {
	Embedded     bool   `json:"embedded"`
	NamingScheme string `json:"naming_scheme"`
}

type Plugin struct {
	plugin.MattermostPlugin

	// configurationLock synchronizes access to the configuration.
	configurationLock sync.RWMutex

	// configuration is the active plugin configuration. Consult getConfiguration and
	// setConfiguration for usage.
	configuration *configuration
}

func (p *Plugin) OnActivate() error {
	config := p.getConfiguration()
	if err := config.IsValid(); err != nil {
		return err
	}

	if err := p.API.RegisterCommand(createJitsiCommand()); err != nil {
		return err
	}

	return nil
}

type YourlsLinkCreationResponse struct {
	ShortUrl string `json:"shorturl"`
}

func shortenUrl(longUrl string) string {
	timeNowSecs := time.Now().Unix()
	timeNow := strconv.FormatInt(timeNowSecs, 10)

	hashFunc := md5.New()
	io.WriteString(hashFunc, timeNow)
	io.WriteString(hashFunc, "05e2685fc7")
	hashSig := hex.EncodeToString(hashFunc.Sum(nil))

	reqUrl := "https://sgmeet.heavenlycloud.online/yourls-api.php?timestamp=" + timeNow + "&signature=" + hashSig + "&action=shorturl&format=json&"
	reqUrl = reqUrl + "url=" + url.QueryEscape(longUrl)
	res, err := http.Get(reqUrl)
	if err != nil {
		panic(err)
	}
	defer res.Body.Close()
	parsedBody := YourlsLinkCreationResponse{}
	json.NewDecoder(res.Body).Decode(&parsedBody)
	return parsedBody.ShortUrl
	//https://sgmeet.heavenlycloud.online/yourls-api.php?signature=05e2685fc7&action=shorturl&format=json&url=https://www.google.com
}

type User struct {
	Avatar string `json:"avatar"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	ID     string `json:"id"`
}

type Context struct {
	User  User   `json:"user"`
	Group string `json:"group"`
}

type EnrichMeetingJwtRequest struct {
	Jwt string `json:"jwt"`
}

// Claims extents cristalhq/jwt standard claims to add jitsi-web-token specific fields
type Claims struct {
	jwt.StandardClaims
	Context Context `json:"context"`
	Room    string  `json:"room,omitempty"`
}

func verifyJwt(secret string, jwtToken string) (*Claims, error) {
	verifier, err := jwt.NewVerifierHS(jwt.HS256, []byte(secret))
	if err != nil {
		mlog.Error("Error generating new HS256 signer", mlog.Err(err))
		return nil, err
	}

	newToken, err := jwt.ParseAndVerifyString(jwtToken, verifier)
	if err != nil {
		mlog.Error("Error parsing or verifiying jwt", mlog.Err(err))
		return nil, err
	}

	var claims Claims
	if err = json.Unmarshal(newToken.RawClaims(), &claims); err != nil {
		mlog.Error("Error unmarshalling claims from jwt", mlog.Err(err))
		return nil, err
	}
	return &claims, nil
}

func signClaims(secret string, claims *Claims) (string, error) {
	signer, err := jwt.NewSignerHS(jwt.HS256, []byte(secret))
	if err != nil {
		mlog.Error("Error generating new HS256 signer", mlog.Err(err))
		return "", errors.New("internal error")
	}
	builder := jwt.NewBuilder(signer)
	token, err := builder.Build(claims)
	if err != nil {
		return "", err
	}
	return string(token.Raw()), nil
}

func (p *Plugin) deleteEphemeralPost(userID, postID string) {
	p.API.DeleteEphemeralPost(userID, postID)
}

func (p *Plugin) updateJwtUserInfo(jwtToken string, user *model.User) (string, error) {
	secret := p.getConfiguration().JitsiAppSecret
	sanitizedUser := user.DeepCopy()

	claims, err := verifyJwt(secret, jwtToken)
	if err != nil {
		return "", err
	}

	config := p.API.GetConfig()
	if config.PrivacySettings.ShowFullName == nil || !*config.PrivacySettings.ShowFullName {
		sanitizedUser.FirstName = ""
		sanitizedUser.LastName = ""
	}
	if config.PrivacySettings.ShowEmailAddress == nil || !*config.PrivacySettings.ShowEmailAddress {
		sanitizedUser.Email = ""
	}
	newContext := Context{
		User: User{
			Avatar: fmt.Sprintf("%s/api/v4/users/%s/image?_=%d", *config.ServiceSettings.SiteURL, sanitizedUser.Id, sanitizedUser.LastPictureUpdate),
			Name:   sanitizedUser.GetDisplayName(model.SHOW_NICKNAME_FULLNAME),
			Email:  sanitizedUser.Email,
			ID:     sanitizedUser.Id,
		},
		Group: claims.Context.Group,
	}

	claims.Context = newContext

	return signClaims(secret, claims)
}

func (p *Plugin) startMeeting(user *model.User, channel *model.Channel, meetingID string, meetingTopic string, personal bool) (string, error) {
	if meetingID == "" {
		meetingID = encodeJitsiMeetingID(meetingTopic)
	}
	meetingPersonal := false

	var baseURL string
	var isJWTEnabled bool
	var jwtValidPeriod int
	var jitsiAppID string
	var jitsiAppSecret string
	if strings.Contains(p.getConfiguration().TeamID, channel.TeamId) {
		baseURL = p.getConfiguration().JitsiURL
		isJWTEnabled = p.getConfiguration().JitsiJWT
		jwtValidPeriod = p.getConfiguration().JitsiLinkValidTime
		jitsiAppID = p.getConfiguration().JitsiAppID
		jitsiAppSecret = p.getConfiguration().JitsiAppSecret
	} else {
		baseURL = p.getConfiguration().JitsiURL2
		isJWTEnabled = p.getConfiguration().JitsiJWT2
		jwtValidPeriod = p.getConfiguration().JitsiLinkValidTime2
		jitsiAppID = p.getConfiguration().JitsiAppID2
		jitsiAppSecret = p.getConfiguration().JitsiAppSecret2
	}

	if len(meetingTopic) < 1 {
		userConfig, err := p.getUserConfig(user.Id)
		if err != nil {
			return "", err
		}

		switch userConfig.NamingScheme {
		case jitsiNameSchemaEnglish:
			meetingID = generateEnglishTitleName()
		case jitsiNameSchemaUUID:
			meetingID = generateUUIDName()
		case jitsiNameSchemaMattermost:
			if channel.Type == model.CHANNEL_DIRECT || channel.Type == model.CHANNEL_GROUP {
				meetingID = generatePersonalMeetingName(user.Username, user.Id)
				meetingTopic = fmt.Sprintf("%s's Personal Meeting", user.GetDisplayName(model.SHOW_NICKNAME_FULLNAME))
				meetingPersonal = true
			} else {
				team, teamErr := p.API.GetTeam(channel.TeamId)
				if teamErr != nil {
					return "", teamErr
				}
				meetingTopic = fmt.Sprintf("%s Channel Meeting", channel.DisplayName)
				meetingID = generateTeamChannelName(team.Name, channel.Name)
			}
		default:
			meetingID = generateEnglishTitleName()
		}
	}
	jitsiURL := strings.TrimSpace(baseURL)
	jitsiURL = strings.TrimRight(jitsiURL, "/")
	meetingURL := jitsiURL + "/" + meetingID
	//meetingLink := meetingURL

	var meetingLinkValidUntil = time.Time{}
	JWTMeeting := isJWTEnabled
	var jwtToken string

	if JWTMeeting {
		// Error check is done in configuration.IsValid()
		jURL, _ := url.Parse(baseURL)

		meetingLinkValidUntil = time.Now().Add(time.Duration(jwtValidPeriod) * time.Minute)

		claims := Claims{}
		claims.Issuer = jitsiAppID
		claims.Audience = []string{jitsiAppID}
		claims.ExpiresAt = jwt.NewNumericDate(meetingLinkValidUntil)
		claims.Subject = jURL.Hostname()
		claims.Room = meetingID

		var err2 error
		jwtToken, err2 = signClaims(jitsiAppSecret, &claims)
		if err2 != nil {
			return "", err2
		}

		meetingURL = meetingURL + "?jwt=" + jwtToken
	}
	meetingURL = meetingURL + "#config.callDisplayName=\"" + meetingTopic + "\""

	meetingLink := meetingURL
	shortenUrl := shortenUrl(meetingURL)
	meetingURL = shortenUrl

	meetingUntil := ""
	if JWTMeeting {
		meetingUntil = "Meeting link valid until: " + meetingLinkValidUntil.Format("Mon Jan 2 , 15:04:05")
	}

	meetingTypeString := "Meeting Link"
	if meetingPersonal {
		meetingTypeString = "Personal Meeting ID (PMI)"
	}

	slackAttachment := model.SlackAttachment{
		Fallback: fmt.Sprintf("Video Meeting started at [%s](%s).\n\n[Join Meeting](%s)\n\n%s", meetingID, meetingURL, meetingURL, meetingUntil),
		Title:    meetingTopic,
		Text:     fmt.Sprintf("%s: [%s](%s)\n\n[Join Meeting](%s)\n\n%s", meetingTypeString, meetingURL, meetingURL, meetingURL, meetingUntil),
	}

	post := &model.Post{
		UserId:    user.Id,
		ChannelId: channel.Id,
		Type:      "custom_jitsi",
		Props: map[string]interface{}{
			"attachments":             []*model.SlackAttachment{&slackAttachment},
			"meeting_id":              meetingID,
			"meeting_link":            meetingURL,
			"meeting_raw_link":        meetingLink,
			"jwt_meeting":             JWTMeeting,
			"meeting_jwt":             jwtToken,
			"jwt_meeting_valid_until": meetingLinkValidUntil.Unix(),
			"meeting_personal":        meetingPersonal,
			"meeting_topic":           meetingTopic,
			"from_webhook":            "true",
			"override_username":       "Jitsi",
			"override_icon_url":       "https://s3.amazonaws.com/mattermost-plugin-media/Zoom+App.png",
		},
	}

	if _, err := p.API.CreatePost(post); err != nil {
		return "", err
	}

	return meetingID, nil
}

// MarshalBinary default marshaling to JSON.
func (c Claims) MarshalBinary() (data []byte, err error) {
	return json.Marshal(c)
}

func encodeJitsiMeetingID(meeting string) string {
	reg := regexp.MustCompile("[^a-zA-Z0-9]+")
	meeting = strings.Replace(meeting, " ", "-", -1)
	return reg.ReplaceAllString(meeting, "")
}

func (p *Plugin) askMeetingType(user *model.User, channel *model.Channel) error {
	apiURL := *p.API.GetConfig().ServiceSettings.SiteURL + "/plugins/jitsi/api/v1/meetings"

	actions := []*model.PostAction{}

	var team *model.Team
	if channel.TeamId != "" {
		team, _ = p.API.GetTeam(channel.TeamId)
	}

	actions = append(actions, &model.PostAction{
		Name: "Meeting name with random words",
		Integration: &model.PostActionIntegration{
			URL: apiURL,
			Context: map[string]interface{}{
				"meeting_id":    generateEnglishTitleName(),
				"meeting_topic": "Jitsi Meeting",
				"personal":      true,
			},
		},
	})

	actions = append(actions, &model.PostAction{
		Name: "Personal meeting",
		Integration: &model.PostActionIntegration{
			URL: apiURL,
			Context: map[string]interface{}{
				"meeting_id":    generatePersonalMeetingName(user.Username, user.Id),
				"meeting_topic": fmt.Sprintf("%s's Meeting", user.GetDisplayName(model.SHOW_NICKNAME_FULLNAME)),
				"personal":      true,
			},
		},
	})

	if channel.Type == model.CHANNEL_OPEN || channel.Type == model.CHANNEL_PRIVATE {
		actions = append(actions, &model.PostAction{
			Name: "Channel meeting",
			Integration: &model.PostActionIntegration{
				URL: apiURL,
				Context: map[string]interface{}{
					"meeting_id":    generateTeamChannelName(team.Name, channel.Name),
					"meeting_topic": fmt.Sprintf("%s Channel Meeting", channel.DisplayName),
					"personal":      false,
				},
			},
		})
	}

	actions = append(actions, &model.PostAction{
		Name: "Meeting name with UUID",
		Integration: &model.PostActionIntegration{
			URL: apiURL,
			Context: map[string]interface{}{
				"meeting_id":    generateUUIDName(),
				"meeting_topic": "Jitsi Meeting",
				"personal":      false,
			},
		},
	})

	sa := model.SlackAttachment{
		Title:   "Jitsi Meeting Start",
		Text:    "Select type of meeting you want to start",
		Actions: actions,
	}

	post := &model.Post{
		UserId:    user.Id,
		ChannelId: channel.Id,
	}
	post.SetProps(map[string]interface{}{
		"attachments": []*model.SlackAttachment{&sa},
	})
	_ = p.API.SendEphemeralPost(user.Id, post)

	return nil
}

func (p *Plugin) getUserConfig(userID string) (*UserConfig, error) {
	data, appErr := p.API.KVGet("config_" + userID)
	if appErr != nil {
		return nil, appErr
	}

	if data == nil {
		return &UserConfig{
			Embedded:     p.getConfiguration().JitsiEmbedded,
			NamingScheme: p.getConfiguration().JitsiNamingScheme,
		}, nil
	}

	var userConfig UserConfig
	err := json.Unmarshal(data, &userConfig)
	if err != nil {
		return nil, err
	}

	return &userConfig, nil
}

func (p *Plugin) setUserConfig(userID string, config *UserConfig) error {
	b, err := json.Marshal(config)
	if err != nil {
		return err
	}

	appErr := p.API.KVSet("config_"+userID, b)
	if appErr != nil {
		return appErr
	}

	p.API.PublishWebSocketEvent(configChangeEvent, nil, &model.WebsocketBroadcast{UserId: userID})
	return nil
}
