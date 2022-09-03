package apgjb

import (
	"context"
	"fmt"
	"net/url"

	"github.com/vmware/govmomi/session"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/soap"
)

type esxClient struct {
	*vim25.Client
	SessionManager *session.Manager
}

func (e *esxClient) Login(ctx context.Context, u *url.Userinfo) error {
	return e.SessionManager.Login(ctx, u)
}

func (e *esxClient) Logout(ctx context.Context) error {
	defer e.Client.CloseIdleConnections()
	return e.SessionManager.Logout(ctx)
}

func newEsxClient(ctx context.Context, uri, user, pass string) *esxClient {
	u, _ := soap.ParseURL(uri)
	u.User = url.UserPassword(user, pass)
	soapClient := soap.NewClient(u, true)
	vimClient, _ := vim25.NewClient(ctx, soapClient)
	client := &esxClient{
		Client:         vimClient,
		SessionManager: session.NewManager(vimClient),
	}
	err := client.Login(ctx, u.User)
	if err != nil {
		fmt.Println(err)
	}
	return client
}
