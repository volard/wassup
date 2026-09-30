package telegramsync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gotd/td/tg"
)

type fakeAPI struct {
	telegramAPI
	users       []tg.UserClass
	result      tg.MessagesMessagesClass
	search      *tg.MessagesSearchRequest
	userRequest []tg.InputUserClass
}

func (f *fakeAPI) UsersGetUsers(_ context.Context, users []tg.InputUserClass) ([]tg.UserClass, error) {
	f.userRequest = users
	return f.users, nil
}
func (f *fakeAPI) MessagesSearch(_ context.Context, request *tg.MessagesSearchRequest) (tg.MessagesMessagesClass, error) {
	f.search = request
	return f.result, nil
}
func TestRemoteSearchUsesStableIDAndOutgoingFilter(t *testing.T) {
	u := &tg.User{ID: 42, Username: "changed_name"}
	u.SetAccessHash(789)
	msg := &tg.Message{ID: 2, Out: true, Date: 1700000000, PeerID: &tg.PeerUser{UserID: 42}}
	api := &fakeAPI{users: []tg.UserClass{u}, result: &tg.MessagesMessagesSlice{Count: 1, Messages: []tg.MessageClass{msg}}}
	remote := &remoteClient{api: api}
	date, err := remote.LatestOutgoing(context.Background(), Person{ID: 42, AccessHash: 123, Username: "old_name"})
	if err != nil {
		t.Fatal(err)
	}
	if date.Unix() != 1700000000 {
		t.Fatal(date)
	}
	if api.userRequest[0].(*tg.InputUser).UserID != 42 {
		t.Fatal("wrong ID")
	}
	peer := api.search.Peer.(*tg.InputPeerUser)
	if peer.UserID != 42 || peer.AccessHash != 789 || api.search.Limit != 1 || api.search.Q != "" {
		t.Fatal(api.search)
	}
	from, ok := api.search.GetFromID()
	if !ok {
		t.Fatal("missing outgoing filter")
	}
	if _, ok := from.(*tg.InputPeerSelf); !ok {
		t.Fatalf("filter %T", from)
	}
	if _, ok := api.search.Filter.(*tg.InputMessagesFilterEmpty); !ok {
		t.Fatal("wrong filter")
	}
}
func TestOutgoingDateRejectsUnexpectedMessages(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result tg.MessagesMessagesClass
	}{
		{"incoming", &tg.MessagesMessages{Messages: []tg.MessageClass{&tg.Message{Date: 1, PeerID: &tg.PeerUser{UserID: 42}}}}},
		{"other user", &tg.MessagesMessages{Messages: []tg.MessageClass{&tg.Message{Out: true, Date: 1, PeerID: &tg.PeerUser{UserID: 43}}}}},
		{"group", &tg.MessagesMessages{Messages: []tg.MessageClass{&tg.Message{Out: true, Date: 1, PeerID: &tg.PeerChat{ChatID: 42}}}}},
		{"service", &tg.MessagesMessages{Messages: []tg.MessageClass{&tg.MessageService{}}}},
		{"not modified", &tg.MessagesMessagesNotModified{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := outgoingDate(tc.result, 42); err == nil {
				t.Fatal("accepted unexpected response")
			}
		})
	}
	date, err := outgoingDate(&tg.MessagesMessages{}, 42)
	if err != nil || !date.IsZero() {
		t.Fatal(date, err)
	}
}
func TestPeopleExcludeBotsSelfDeletedAndMinimal(t *testing.T) {
	for _, u := range []*tg.User{{ID: 1, Bot: true}, {ID: 1, Self: true}, {ID: 1, Deleted: true}, {ID: 1, Min: true}} {
		u.SetAccessHash(12)
		if _, ok := personFromUser(u); ok {
			t.Fatal("accepted ineligible user")
		}
	}
	if _, ok := personFromUser(&tg.User{ID: 1}); ok {
		t.Fatal("missing access hash accepted")
	}
}
func TestRateLimitWaitHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	remote := &remoteClient{nextCall: time.Now().Add(time.Hour)}
	if _, _, err := remote.requestContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

type peopleAPI struct {
	telegramAPI
	requests []*tg.MessagesGetDialogsRequest
}

func testUser(id int64, name string) *tg.User {
	u := &tg.User{ID: id, FirstName: name}
	u.SetAccessHash(id * 10)
	return u
}
func (f *peopleAPI) ContactsGetContacts(context.Context, int64) (tg.ContactsContactsClass, error) {
	bot := testUser(9, "Bot")
	bot.Bot = true
	return &tg.ContactsContacts{Users: []tg.UserClass{testUser(1, "Contact"), bot}}, nil
}
func (f *peopleAPI) MessagesGetDialogs(_ context.Context, req *tg.MessagesGetDialogsRequest) (tg.MessagesDialogsClass, error) {
	f.requests = append(f.requests, req)
	folder, _ := req.GetFolderID()
	id := int64(2)
	if folder == 1 {
		id = 4
	} else if req.OffsetID != 0 {
		id = 3
	}
	user := testUser(id, "Chat")
	dialog := &tg.Dialog{Peer: &tg.PeerUser{UserID: id}, TopMessage: int(id)}
	message := &tg.Message{ID: int(id), Date: int(id), PeerID: &tg.PeerUser{UserID: id}}
	if folder == 0 && req.OffsetID == 0 {
		return &tg.MessagesDialogsSlice{Count: 2, Dialogs: []tg.DialogClass{dialog}, Users: []tg.UserClass{user}, Messages: []tg.MessageClass{message}}, nil
	}
	return &tg.MessagesDialogs{Dialogs: []tg.DialogClass{dialog}, Users: []tg.UserClass{user}, Messages: []tg.MessageClass{message}}, nil
}
func TestPeopleIncludesPaginatedAndArchivedPrivateChats(t *testing.T) {
	api := &peopleAPI{}
	remote := &remoteClient{api: api}
	people, err := remote.People(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ids := map[int64]bool{}
	for _, p := range people {
		ids[p.ID] = true
	}
	if len(ids) != 4 || !ids[1] || !ids[2] || !ids[3] || !ids[4] {
		t.Fatal(people)
	}
	if len(api.requests) != 3 {
		t.Fatal(len(api.requests))
	}
	if api.requests[1].OffsetID != 2 || !api.requests[1].ExcludePinned {
		t.Fatal("pagination did not advance")
	}
	if folder, ok := api.requests[2].GetFolderID(); !ok || folder != 1 {
		t.Fatal("missing archive query")
	}
}
