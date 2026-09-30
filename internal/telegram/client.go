package telegramsync

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
)

type telegramAPI interface {
	UsersGetFullUser(context.Context, tg.InputUserClass) (*tg.UsersUserFull, error)
	ContactsGetContacts(context.Context, int64) (tg.ContactsContactsClass, error)
	ContactsResolveUsername(context.Context, *tg.ContactsResolveUsernameRequest) (*tg.ContactsResolvedPeer, error)
	ContactsResolvePhone(context.Context, string) (*tg.ContactsResolvedPeer, error)
	MessagesGetDialogs(context.Context, *tg.MessagesGetDialogsRequest) (tg.MessagesDialogsClass, error)
	UsersGetUsers(context.Context, []tg.InputUserClass) ([]tg.UserClass, error)
	MessagesSearch(context.Context, *tg.MessagesSearchRequest) (tg.MessagesMessagesClass, error)
}

type remoteClient struct {
	api      telegramAPI
	nextCall time.Time
}

func (r *remoteClient) requestContext(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if delay := time.Until(r.nextCall); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-timer.C:
		}
	}
	r.nextCall = time.Now().Add(500 * time.Millisecond)
	child, cancel := context.WithTimeout(ctx, 20*time.Second)
	return child, cancel, nil
}

func personFromUser(u *tg.User) (Person, bool) {
	hash, ok := u.GetAccessHash()
	if !ok || u.Min || u.Bot || u.Self || u.Deleted || u.ID <= 0 {
		return Person{}, false
	}
	return Person{ID: u.ID, AccessHash: hash, Name: strings.TrimSpace(u.FirstName + " " + u.LastName), Username: u.Username, Phone: u.Phone}, true
}

func (r *remoteClient) People(ctx context.Context) ([]Person, error) {
	callCtx, cancel, err := r.requestContext(ctx)
	if err != nil {
		return nil, err
	}
	result, err := r.api.ContactsGetContacts(callCtx, 0)
	cancel()
	if err != nil {
		return nil, err
	}
	list, ok := result.(*tg.ContactsContacts)
	if !ok {
		return nil, fmt.Errorf("unexpected contacts response %T", result)
	}
	people := map[int64]Person{}
	add := func(users []tg.UserClass) {
		for _, v := range users {
			if u, ok := v.(*tg.User); ok {
				if p, ok := personFromUser(u); ok {
					people[p.ID] = p
				}
			}
		}
	}
	add(list.Users)
	// Include private chats with people not saved as contacts, plus archived chats.
	for _, folder := range []int{0, 1} {
		complete := false
		iter := dialogs.NewIterator(dialogs.QueryFunc(func(ctx context.Context, req dialogs.Request) (tg.MessagesDialogsClass, error) {
			// The iterator asks once more after consuming its final batch.
			if complete {
				return &tg.MessagesDialogs{}, nil
			}
			callCtx, cancel, err := r.requestContext(ctx)
			if err != nil {
				return nil, err
			}
			defer cancel()
			request := &tg.MessagesGetDialogsRequest{OffsetID: req.OffsetID, OffsetDate: req.OffsetDate, OffsetPeer: req.OffsetPeer, Limit: req.Limit, ExcludePinned: req.OffsetID != 0}
			request.SetFolderID(folder)
			result, err := r.api.MessagesGetDialogs(callCtx, request)
			if err == nil {
				switch page := result.(type) {
				case *tg.MessagesDialogs:
					complete = true
				case *tg.MessagesDialogsSlice:
					complete = len(page.Dialogs) == 0
				}
			}
			return result, err
		}), 100)
		for iter.Next(ctx) {
			elem := iter.Value()
			peer, ok := elem.Peer.(*tg.InputPeerUser)
			if !ok {
				continue
			}
			if u, ok := elem.Entities.Users()[peer.UserID]; ok {
				if p, ok := personFromUser(u); ok {
					people[p.ID] = p
				}
			}
		}
		if err := iter.Err(); err != nil {
			return nil, err
		}
	}
	out := make([]Person, 0, len(people))
	for _, p := range people {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

func (r *remoteClient) Resolve(ctx context.Context, handle string) (Person, error) {
	handle = username(handle)
	if handle == "" {
		return Person{}, errors.New("enter a Telegram username or t.me profile URL")
	}
	callCtx, cancel, err := r.requestContext(ctx)
	if err != nil {
		return Person{}, err
	}
	defer cancel()
	result, err := r.api.ContactsResolveUsername(callCtx, &tg.ContactsResolveUsernameRequest{Username: handle})
	if err != nil {
		return Person{}, err
	}
	peer, ok := result.Peer.(*tg.PeerUser)
	if !ok {
		return Person{}, errors.New("only personal chats can be linked")
	}
	for _, v := range result.Users {
		if u, ok := v.(*tg.User); ok && u.ID == peer.UserID {
			if p, ok := personFromUser(u); ok {
				return p, nil
			}
		}
	}
	return Person{}, errors.New("cannot link a bot, deleted account, or your own account")
}

func (r *remoteClient) LatestOutgoing(ctx context.Context, p Person) (time.Time, error) {
	// Resolve by stable ID, never a username that could have changed owners.
	callCtx, cancel, err := r.requestContext(ctx)
	if err != nil {
		return time.Time{}, err
	}
	users, err := r.api.UsersGetUsers(callCtx, []tg.InputUserClass{&tg.InputUser{UserID: p.ID, AccessHash: p.AccessHash}})
	cancel()
	if err != nil {
		return time.Time{}, fmt.Errorf("check linked account (relink if access changed): %w", err)
	}
	if len(users) != 1 {
		return time.Time{}, errors.New("linked Telegram account was not returned")
	}
	u, ok := users[0].(*tg.User)
	if !ok {
		return time.Time{}, errors.New("linked Telegram account is unavailable")
	}
	current, ok := personFromUser(u)
	if !ok || current.ID != p.ID {
		return time.Time{}, errors.New("linked account is no longer an eligible personal contact")
	}
	callCtx, cancel, err = r.requestContext(ctx)
	if err != nil {
		return time.Time{}, err
	}
	defer cancel()
	request := &tg.MessagesSearchRequest{Peer: &tg.InputPeerUser{UserID: current.ID, AccessHash: current.AccessHash}, Filter: &tg.InputMessagesFilterEmpty{}, Limit: 1}
	request.SetFromID(&tg.InputPeerSelf{})
	result, err := r.api.MessagesSearch(callCtx, request)
	if err != nil {
		return time.Time{}, err
	}
	return outgoingDate(result, current.ID)
}

func outgoingDate(result tg.MessagesMessagesClass, peerID int64) (time.Time, error) {
	var messages []tg.MessageClass
	switch v := result.(type) {
	case *tg.MessagesMessages:
		messages = v.Messages
	case *tg.MessagesMessagesSlice:
		messages = v.Messages
	default:
		return time.Time{}, fmt.Errorf("unexpected private history response %T", result)
	}
	if len(messages) == 0 {
		return time.Time{}, nil
	}
	m, ok := messages[0].(*tg.Message)
	if !ok {
		return time.Time{}, errors.New("Telegram search returned a non-message result")
	}
	peer, ok := m.PeerID.(*tg.PeerUser)
	if !ok || peer.UserID != peerID || !m.Out || m.Date <= 0 {
		return time.Time{}, errors.New("Telegram search returned a message outside the requested outgoing personal chat")
	}
	return time.Unix(int64(m.Date), 0), nil
}
