package main

import (
	"fmt"
	"strings"

	"github.com/mattermost/mattermost-server/v5/mlog"
	"github.com/mattermost/mattermost-server/v5/model"
	"github.com/mattermost/mattermost-server/v5/plugin"
)

const jitsiCommand = "meet"
const commandHelp = `* |/` + jitsiCommand + `| - Create a new video conference
* |/` + jitsiCommand + ` [topic]| - Create a new video conference with specified topic
* |/` + jitsiCommand + ` help| - Show this help text
* |/` + jitsiCommand + ` settings| - View your current user settings for the Jitsi plugin
* |/` + jitsiCommand + ` settings [setting] [value]| - Update your user settings (see below for options)

###### Jitsi Settings:
* |/` + jitsiCommand + ` settings embedded [true/false]|: When true, Jitsi meeting is embedded as a floating window inside Mattermost. When false, Jitsi meeting opens in a new window.
* |/` + jitsiCommand + ` settings naming_scheme [words/uuid/mattermost/ask]|: Select how meeting names are generated with one of these options:
    * |words|: Random English words in title case (e.g. PlayfulDragonsObserveCuriously)
    * |uuid|: UUID (universally unique identifier)
    * |mattermost|: Mattermost specific names. Combination of team name, channel name and random text in public and private channels; personal meeting name in direct and group messages channels.
    * |ask|: The plugin asks you to select the name every time you start a meeting`

func startMeetingError(channelID string, detailedError string) (*model.CommandResponse, *model.AppError) {
	return &model.CommandResponse{
			ResponseType: model.COMMAND_RESPONSE_TYPE_EPHEMERAL,
			ChannelId:    channelID,
			Text:         "We could not start a meeting at this time.",
		}, &model.AppError{
			Message:       "We could not start a meeting at this time.",
			DetailedError: detailedError,
		}
}

func createJitsiCommand() *model.Command {
	return &model.Command{
		Trigger:          jitsiCommand,
		AutoComplete:     true,
		AutoCompleteDesc: "Start a Video Conference in current channel. Other available commands: help, settings",
		AutoCompleteHint: "[command]",
	}
}

func (p *Plugin) ExecuteCommand(c *plugin.Context, args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	split := strings.Fields(args.Command)
	command := split[0]
	var parameters []string
	action := ""
	if len(split) > 1 {
		action = split[1]
	}
	if len(split) > 2 {
		parameters = split[2:]
	}

	if command != "/"+jitsiCommand {
		return &model.CommandResponse{}, nil
	}

	if action == "help" {
		return p.executeHelpCommand(c, args)
	}

	if action == "settings" {
		return p.executeSettingsCommand(c, args, parameters)
	}

	return p.executeStartMeetingCommand(c, args)
}

func (p *Plugin) executeStartMeetingCommand(c *plugin.Context, args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	input := strings.TrimSpace(strings.TrimPrefix(args.Command, "/"+jitsiCommand))

	user, appErr := p.API.GetUser(args.UserId)
	if appErr != nil {
		return startMeetingError(args.ChannelId, fmt.Sprintf("getUser() threw error: %s", appErr))
	}

	channel, appErr := p.API.GetChannel(args.ChannelId)
	if appErr != nil {
		return startMeetingError(args.ChannelId, fmt.Sprintf("getChannel() threw error: %s", appErr))
	}

	userConfig, err := p.getUserConfig(args.UserId)
	if err != nil {
		return startMeetingError(args.ChannelId, fmt.Sprintf("getChannel() threw error: %s", err))
	}

	if userConfig.NamingScheme == jitsiNameSchemaAsk && input == "" {
		if err := p.askMeetingType(user, channel); err != nil {
			return startMeetingError(args.ChannelId, fmt.Sprintf("startMeeting() threw error: %s", appErr))
		}
	} else {
		if _, err := p.startMeeting(user, channel, "", input, false); err != nil {
			return startMeetingError(args.ChannelId, fmt.Sprintf("startMeeting() threw error: %s", appErr))
		}
	}

	return &model.CommandResponse{}, nil
}

func (p *Plugin) executeHelpCommand(c *plugin.Context, args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	text := "###### Mattermost Jitsi Plugin - Slash Command Help\n" + strings.Replace(commandHelp, "|", "`", -1)
	post := &model.Post{
		UserId:    args.UserId,
		ChannelId: args.ChannelId,
		Message:   text,
	}
	_ = p.API.SendEphemeralPost(args.UserId, post)

	return &model.CommandResponse{}, nil
}

func (p *Plugin) settingsError(userID string, channelID string, errorText string) (*model.CommandResponse, *model.AppError) {
	post := &model.Post{
		UserId:    userID,
		ChannelId: channelID,
		Message:   errorText,
	}
	_ = p.API.SendEphemeralPost(userID, post)

	return &model.CommandResponse{}, nil
}

func (p *Plugin) executeSettingsCommand(c *plugin.Context, args *model.CommandArgs, parameters []string) (*model.CommandResponse, *model.AppError) {
	text := ""

	userConfig, err := p.getUserConfig(args.UserId)
	if err != nil {
		mlog.Debug("Unable to get user config", mlog.Err(err))
		return p.settingsError(args.UserId, args.ChannelId, "Unable to get user settings.")
	}

	if len(parameters) == 0 {
		text = fmt.Sprintf("###### Jitsi Settings:\n* Embedded: `%v`\n* Naming Scheme: `%s`", userConfig.Embedded, userConfig.NamingScheme)
		post := &model.Post{
			UserId:    args.UserId,
			ChannelId: args.ChannelId,
			Message:   text,
		}
		_ = p.API.SendEphemeralPost(args.UserId, post)

		return &model.CommandResponse{}, nil
	}

	if len(parameters) != 2 {
		return p.settingsError(args.UserId, args.ChannelId, "Invalid settings parameters")
	}

	switch parameters[0] {
	case "embedded":
		switch parameters[1] {
		case "true":
			userConfig.Embedded = true
		case "false":
			userConfig.Embedded = false
		default:
			text = "Invalid `embedded` value, use `true` or `false`."
			userConfig = nil
			break
		}
	case "naming_scheme":
		switch parameters[1] {
		case jitsiNameSchemaAsk:
			userConfig.NamingScheme = "ask"
		case jitsiNameSchemaEnglish:
			userConfig.NamingScheme = "english-titlecase"
		case jitsiNameSchemaUUID:
			userConfig.NamingScheme = "uuid"
		case jitsiNameSchemaMattermost:
			userConfig.NamingScheme = "mattermost"
		default:
			text = "Invalid `naming_scheme` value, use `ask`, `english-titlecase`, `uuid` or `mattermost`."
			userConfig = nil
		}
	default:
		text = "Invalid config field, use `embedded` or `naming_scheme`."
		userConfig = nil
	}

	if userConfig == nil {
		return p.settingsError(args.UserId, args.ChannelId, text)
	}

	err = p.setUserConfig(args.UserId, userConfig)
	if err != nil {
		mlog.Debug("Unable to set user settings", mlog.Err(err))
		return p.settingsError(args.UserId, args.ChannelId, "Unable to set user settings")
	}

	post := &model.Post{
		UserId:    args.UserId,
		ChannelId: args.ChannelId,
		Message:   "Jitsi settings updated",
	}
	_ = p.API.SendEphemeralPost(args.UserId, post)

	return &model.CommandResponse{}, nil
}
