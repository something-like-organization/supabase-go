package supabase

import (
	"errors"
	"log"
	"time"

	"github.com/supabase-community/auth-go"
	"github.com/supabase-community/auth-go/types"
	"github.com/supabase-community/functions-go"
	"github.com/supabase-community/postgrest-go"
	storage_go "github.com/supabase-community/storage-go"
)

const (
	REST_URL      = "/rest/v1"
	STORAGE_URL   = "/storage/v1"
	AUTH_URL      = "/auth/v1"
	FUNCTIONS_URL = "/functions/v1"
)

type Client struct {
	// Why is this a private field??
	rest    *postgrest.Client
	Storage *storage_go.Client
	// Auth is an interface. We don't need a pointer to an interface.
	Auth      auth.Client
	Functions *functions.Client
	options   clientOptions
}

type clientOptions struct {
	url     string
	headers map[string]string
	schema  string
}

type ClientOptions struct {
	Headers map[string]string
	Schema  string
}

// NewClient creates a new Supabase client.
// url is the Supabase URL.
// key is the Supabase API key.
// options is the Supabase client options.
func NewClient(url, key string, options *ClientOptions) (*Client, error) {

	if url == "" || key == "" {
		return nil, errors.New("url and key are required")
	}

	headers := map[string]string{
		"Authorization": "Bearer " + key,
		"apikey":        key,
	}

	if options != nil && options.Headers != nil {
		for k, v := range options.Headers {
			headers[k] = v
		}
	}

	client := &Client{}
	client.options.url = url
	// map is pass by reference, so this gets updated by rest of function
	client.options.headers = headers

	if options != nil && options.Schema != "" {
		client.options.schema = options.Schema
	} else {
		client.options.schema = "public"
	}

	client.rest = postgrest.NewClient(url+REST_URL, client.options.schema, headers)
	client.Storage = storage_go.NewClient(url+STORAGE_URL, key, headers)
	client.Auth = auth.New(url, key).WithCustomAuthURL(url + AUTH_URL)
	client.Functions = functions.NewClient(url+FUNCTIONS_URL, key, headers)

	return client, nil
}

// Wrap postgrest From method
// From returns a QueryBuilder for the specified table.
func (c *Client) From(table string) *postgrest.QueryBuilder {
	return c.rest.From(table)
}

// Wrap postgrest Rpc method
// Rpc returns a string for the specified function.
func (c *Client) Rpc(name, count string, rpcBody interface{}) string {
	return c.rest.Rpc(name, count, rpcBody)
}

func (c *Client) SignInWithEmailPassword(email, password string) (types.Session, error) {
	resp, err := c.Auth.SignInWithEmailPassword(email, password)
	if err != nil {
		return types.Session{}, err
	}
	c.UpdateAuthSession(resp.Session)

	return resp.Session, err
}

func (c *Client) SignInWithPhonePassword(phone, password string) (types.Session, error) {
	resp, err := c.Auth.SignInWithPhonePassword(phone, password)
	if err != nil {
		return types.Session{}, err
	}
	c.UpdateAuthSession(resp.Session)
	return resp.Session, err
}

func (c *Client) EnableTokenAutoRefresh(session types.Session) {
	go func() {
		attempt := 0
		expiresAt := time.Now().Add(time.Duration(session.ExpiresIn) * time.Second)

		for {
			sleepDuration := (time.Until(expiresAt) / 4) * 3
			if sleepDuration > 0 {
				time.Sleep(sleepDuration)
			}

			// Refresh the token
			newSession, err := c.RefreshToken(session.RefreshToken)
			if err != nil {
				attempt++
				if attempt <= 3 {
					log.Printf("Error refreshing token, retrying with exponential backoff: %v", err)
					time.Sleep(time.Duration(1<<attempt) * time.Second)
				} else {
					log.Printf("Error refreshing token, retrying every 30 seconds: %v", err)
					time.Sleep(30 * time.Second)
				}
				continue
			}

			// Update the session, reset the attempt counter, and update the expiresAt time
			c.UpdateAuthSession(newSession)
			session = newSession
			attempt = 0
			expiresAt = time.Now().Add(time.Duration(session.ExpiresIn) * time.Second)
		}
	}()
}

func (c *Client) RefreshToken(refreshToken string) (types.Session, error) {
	resp, err := c.Auth.RefreshToken(refreshToken)
	if err != nil {
		return types.Session{}, err
	}
	c.UpdateAuthSession(resp.Session)
	return resp.Session, err
}

func (c *Client) UpdateAuthSession(session types.Session) {
	c.Auth = c.Auth.WithToken(session.AccessToken)
	c.rest.SetAuthToken(session.AccessToken)
	c.options.headers["Authorization"] = "Bearer " + session.AccessToken
	c.Storage = storage_go.NewClient(c.options.url+STORAGE_URL, session.AccessToken, c.options.headers)
	c.Functions = functions.NewClient(c.options.url+FUNCTIONS_URL, session.AccessToken, c.options.headers)
}

func (c *Client) WithToken(token string) (*Client, error) {
	if c == nil {
		return nil, errors.New("cannot copy non-initialized client")
	}

	clientCopy := &Client{}

	for key, value := range c.options.headers {
		clientCopy.options.headers[key] = value
	}
	clientCopy.options.headers["Authorization"] = "Bearer " + token
	clientCopy.rest = postgrest.NewClient(
		clientCopy.options.url+REST_URL,
		clientCopy.options.schema,
		clientCopy.options.headers,
	)
	clientCopy.rest.SetAuthToken(token)
	clientCopy.Auth = c.Auth.WithToken(token)
	clientCopy.Storage = storage_go.NewClient(
		clientCopy.options.url+STORAGE_URL,
		token,
		clientCopy.options.headers,
	)
	clientCopy.Functions = functions.NewClient(
		clientCopy.options.url+FUNCTIONS_URL,
		token,
		clientCopy.options.headers,
	)

	return clientCopy, nil
}
