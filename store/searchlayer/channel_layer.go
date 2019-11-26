// Copyright (c) 2017-present Mattermost, Inc. All Rights Reserved.
// See License.txt for license information.

package searchlayer

import (
	"github.com/mattermost/mattermost-server/mlog"
	"github.com/mattermost/mattermost-server/model"
	"github.com/mattermost/mattermost-server/services/searchengine"
	"github.com/mattermost/mattermost-server/store"
)

type SearchChannelStore struct {
	store.ChannelStore
	rootStore *SearchStore
}

func (c *SearchChannelStore) deleteChannelIndex(channel *model.Channel) {
	if channel.Type == model.CHANNEL_OPEN {
		for _, engine := range c.rootStore.searchEngine.GetActiveEngines() {
			if engine.IsIndexingEnabled() {
				engineCopy := engine
				go (func() {
					if err := engineCopy.DeleteChannel(channel); err != nil {
						mlog.Error("Encountered error deleting channel", mlog.String("channel_id", channel.Id), mlog.Err(err))
					}
				})()
			}
		}
	}
}

func (c *SearchChannelStore) indexChannel(channel *model.Channel) {
	if channel.Type == model.CHANNEL_OPEN {
		for _, engine := range c.rootStore.searchEngine.GetActiveEngines() {
			if engine.IsIndexingEnabled() {
				engineCopy := engine
				go (func() {
					if err := engineCopy.IndexChannel(channel); err != nil {
						mlog.Error("Encountered error indexing channel", mlog.String("channel_id", channel.Id), mlog.Err(err))
					}
				})()
			}
		}
	}
}

func (c *SearchChannelStore) Save(channel *model.Channel, maxChannels int64) (*model.Channel, *model.AppError) {
	newChannel, err := c.ChannelStore.Save(channel, maxChannels)
	if err == nil {
		c.indexChannel(newChannel)
	}
	return newChannel, err
}

func (c *SearchChannelStore) Update(channel *model.Channel) (*model.Channel, *model.AppError) {
	updatedChannel, err := c.ChannelStore.Update(channel)
	if err == nil {
		c.indexChannel(updatedChannel)
	}
	return updatedChannel, err
}

func (c *SearchChannelStore) SaveMember(cm *model.ChannelMember) (*model.ChannelMember, *model.AppError) {
	member, err := c.ChannelStore.SaveMember(cm)
	if err != nil {
		channel, channelErr := c.ChannelStore.Get(member.ChannelId, true)
		if channelErr != nil {
			mlog.Error("Encountered error indexing user in channel", mlog.String("channel_id", member.ChannelId), mlog.Err(err))
		} else {
			c.rootStore.indexUserFromID(channel.CreatorId)
		}
	}
	return member, err
}

func (c *SearchChannelStore) RemoveMember(channelId, userIdToRemove string) *model.AppError {
	err := c.ChannelStore.RemoveMember(channelId, userIdToRemove)
	if err == nil {
		c.rootStore.indexUserFromID(userIdToRemove)
	}
	return err
}

func (c *SearchChannelStore) CreateDirectChannel(user *model.User, otherUser *model.User) (*model.Channel, *model.AppError) {
	channel, err := c.ChannelStore.CreateDirectChannel(user, otherUser)
	if err == nil {
		c.rootStore.indexUserFromID(user.Id)
		c.rootStore.indexUserFromID(otherUser.Id)
	}
	return channel, err
}

func (c *SearchChannelStore) AutocompleteInTeam(teamId string, term string, includeDeleted bool) (*model.ChannelList, *model.AppError) {
	var channelList *model.ChannelList
	var err *model.AppError

	allFailed := true
	for _, engine := range c.rootStore.searchEngine.GetActiveEngines() {
		if engine.IsAutocompletionEnabled() {
			channelList, err = c.esAutocompleteChannels(engine, teamId, term, includeDeleted)
			if err != nil {
				mlog.Error("Encountered error on AutocompleteChannels through SearchEngine. Falling back to default autocompletion.", mlog.Err(err))
				continue
			}
			allFailed = false
			break
		}
	}

	if allFailed {
		channelList, err = c.ChannelStore.AutocompleteInTeam(teamId, term, includeDeleted)
		if err != nil {
			return nil, err
		}
	}
	return channelList, err
}

func (c *SearchChannelStore) esAutocompleteChannels(engine searchengine.SearchEngineInterface, teamId, term string, includeDeleted bool) (*model.ChannelList, *model.AppError) {
	channelIds, err := engine.SearchChannels(teamId, term)
	if err != nil {
		return nil, err
	}

	channelList := model.ChannelList{}
	if len(channelIds) > 0 {
		channels, err := c.ChannelStore.GetChannelsByIds(channelIds)
		if err != nil {
			return nil, err
		}
		for _, ch := range channels {
			if ch.DeleteAt > 0 && !includeDeleted {
				continue
			}
			channelList = append(channelList, ch)
		}
	}

	return &channelList, nil
}

func (c *SearchChannelStore) PermanentDeleteMembersByChannel(channelId string) *model.AppError {
	err := c.ChannelStore.PermanentDeleteMembersByChannel(channelId)

	if err == nil {
		profiles, err := c.rootStore.User().GetAllProfilesInChannel(channelId, false)
		if err != nil {
			mlog.Error("Encountered error indexing users for channel", mlog.String("channel_id", channelId), mlog.Err(err))
		} else {
			for _, user := range profiles {
				c.rootStore.indexUser(user)
			}
		}
	}

	return err
}

func (c *SearchChannelStore) PermanentDelete(channelId string) *model.AppError {
	channel, channelErr := c.ChannelStore.Get(channelId, true)
	if channelErr != nil {
		mlog.Error("Encountered error deleting channel", mlog.String("channel_id", channelId), mlog.Err(channelErr))
	}
	err := c.ChannelStore.PermanentDelete(channelId)
	if err == nil {
		c.deleteChannelIndex(channel)
	}
	return err
}
