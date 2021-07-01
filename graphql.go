package graphql

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"

	"golang.org/x/net/context/ctxhttp"

	"github.com/runtimeracer/go-graphql-client/internal/jsonutil"
)

// Client is a GraphQL client.
type Client struct {
	url        string // GraphQL server URL.
	httpClient *http.Client
}

// NewClient creates a GraphQL client targeting the specified GraphQL server URL.
// If httpClient is nil, then http.DefaultClient is used.
func NewClient(url string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		url:        url,
		httpClient: httpClient,
	}
}

// Query executes a single GraphQL query request,
// with a query derived from q, populating the response into it.
// q should be a pointer to struct that corresponds to the GraphQL schema.
func (c *Client) Query(ctx context.Context, q interface{}, variables map[string]interface{}) error {
	return c.do(ctx, queryOperation, q, variables, "")
}

// NamedQuery executes a single GraphQL query request, with operation name
func (c *Client) NamedQuery(ctx context.Context, name string, q interface{}, variables map[string]interface{}) error {
	return c.do(ctx, queryOperation, q, variables, name)
}

// Mutate executes a single GraphQL mutation request,
// with a mutation derived from m, populating the response into it.
// m should be a pointer to struct that corresponds to the GraphQL schema.
func (c *Client) Mutate(ctx context.Context, m interface{}, variables map[string]interface{}) error {
	return c.do(ctx, mutationOperation, m, variables, "")
}

// NamedMutate executes a single GraphQL mutation request, with operation name
func (c *Client) NamedMutate(ctx context.Context, name string, m interface{}, variables map[string]interface{}) error {
	return c.do(ctx, mutationOperation, m, variables, name)
}

// Query executes a single GraphQL query request,
// with a query derived from q, populating the response into it.
// q should be a pointer to struct that corresponds to the GraphQL schema.
// return raw bytes message.
func (c *Client) QueryRaw(ctx context.Context, q interface{}, variables map[string]interface{}) (*json.RawMessage, error) {
	return c.doRaw(ctx, queryOperation, q, variables, "")
}

// NamedQueryRaw executes a single GraphQL query request, with operation name
// return raw bytes message.
func (c *Client) NamedQueryRaw(ctx context.Context, name string, q interface{}, variables map[string]interface{}) (*json.RawMessage, error) {
	return c.doRaw(ctx, queryOperation, q, variables, name)
}

// MutateRaw executes a single GraphQL mutation request,
// with a mutation derived from m, populating the response into it.
// m should be a pointer to struct that corresponds to the GraphQL schema.
// return raw bytes message.
func (c *Client) MutateRaw(ctx context.Context, m interface{}, variables map[string]interface{}) (*json.RawMessage, error) {
	return c.doRaw(ctx, mutationOperation, m, variables, "")
}

// NamedMutateRaw executes a single GraphQL mutation request, with operation name
// return raw bytes message.
func (c *Client) NamedMutateRaw(ctx context.Context, name string, m interface{}, variables map[string]interface{}) (*json.RawMessage, error) {
	return c.doRaw(ctx, mutationOperation, m, variables, name)
}

// do executes a single GraphQL operation.
// return raw message and error
func (c *Client) doRaw(ctx context.Context, op operationType, v interface{}, variables map[string]interface{}, name string) (*json.RawMessage, error) {
	var query string
	switch op {
	case queryOperation:
		query = constructQuery(v, variables, name)
	case mutationOperation:
		query = constructMutation(v, variables, name)
	}
	in := struct {
		Query     string                 `json:"query"`
		Variables map[string]interface{} `json:"variables,omitempty"`
	}{
		Query:     query,
		Variables: variables,
	}
	var buf bytes.Buffer
	err := json.NewEncoder(&buf).Encode(in)
	if err != nil {
		return nil, err
	}
	resp, err := ctxhttp.Post(ctx, c.httpClient, c.url, "application/json", &buf)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("non-200 OK status code: %v body: %q", resp.Status, body)
	}
	var out struct {
		Data   *json.RawMessage
		Errors errors
		//Extensions interface{} // Unused.
	}
	err = json.NewDecoder(resp.Body).Decode(&out)
	if err != nil {
		// TODO: Consider including response body in returned error, if deemed helpful.
		return nil, err
	}

	if len(out.Errors) > 0 {
		return out.Data, out.Errors
	}

	return out.Data, nil
}

// do executes a single GraphQL operation and unmarshal json.
func (c *Client) do(ctx context.Context, op operationType, v interface{}, variables map[string]interface{}, name string) error {

	var query string
	switch op {
	case queryOperation:
		query = constructQuery(v, variables, name)
	case mutationOperation:
		query = constructMutation(v, variables, name)
	}
	in := struct {
		Query     string                 `json:"query"`
		Variables map[string]interface{} `json:"variables,omitempty"`
	}{
		Query:     query,
		Variables: variables,
	}
	var buf bytes.Buffer
	err := json.NewEncoder(&buf).Encode(in)
	if err != nil {
		return err
	}
	resp, err := ctxhttp.Post(ctx, c.httpClient, c.url, "application/json", &buf)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("non-200 OK status code: %v body: %q", resp.Status, body)
	}
	var out graphQLStdOut
	out, err = c.unmarshalGraphQLResult(resp.Body)
	if err != nil {
		// TODO: Consider including response body in returned error, if deemed helpful.
		return err
	}
	if out.Data != nil {
		err := jsonutil.UnmarshalGraphQL(*out.Data, v)
		if err != nil {
			// TODO: Consider including response body in returned error, if deemed helpful.
			return err
		}
	}
	if len(out.Errors) > 0 {
		return out.Errors
	}
	return nil
}

func (c *Client) unmarshalGraphQLResult(responseBody io.Reader) (graphQLStdOut, error) {
	// Try unmarshal into default format
	var output graphQLStdOut
	err := json.NewDecoder(responseBody).Decode(&output)
	if err != nil {
		// TODO: Consider including response body in returned error, if deemed helpful.
		// TODO: Add Warning message somehow that default is not working
		var extFormat graphQLExtOut
		err := json.NewDecoder(responseBody).Decode(&extFormat)
		if err != nil {
			// Output too weird or query error
			return output, err
		}

		// Convert Ext to default to meet criteria
		output = graphQLStdOut{
			Data:       extFormat.Data,
			Errors:     extFormat.Errors.ConvertToStandard(),
			Extensions: extFormat.Extensions,
		}
	}
	return output, nil
}

type graphQLStdOut struct {
	Data       *json.RawMessage
	Errors     errors
	Extensions interface{}
}

type graphQLExtOut struct {
	Data       *json.RawMessage
	Errors     errorsExt
	Extensions interface{}
}

// errors represents the "errors" array in a response from a GraphQL server.
// If returned via error interface, the slice is expected to contain at least 1 element.
//
// Specification: https://facebook.github.io/graphql/#sec-Errors.
type errors []errorStruct
type errorStruct struct {
	Message   string
	Locations []struct {
		Line   int
		Column int
	}
}

// Error implements error interface.
func (e errors) Error() string {
	if len(e) == 0 {
		return ""
	}
	return e[0].Message
}

// errorsExt represents the "errors" array in a response from a GraphQL server.
// If returned via error interface, the slice is expected to contain at least 1 element.
// The "Ext" variant of this struct is able to handle non-standard implementations of the error message
type errorsExt []errorsExtStruct
type errorsExtStruct struct {
	Message   []interface{}
	Locations []struct {
		Line   int
		Column int
	}
}

// Error implements error interface.
func (e errorsExt) Error() string {
	if len(e) == 0 {
		return ""
	}

	var stringOutput = make([]string, len(e[0].Message))
	for i := range e[0].Message {
		stringOutput[i] = fmt.Sprintf("%v", e[0].Message[i])
	}
	return strings.Join(stringOutput, ";")
}

// ConvertToStandard translates extended error structs into structs matching the GraphQL Standard
func (e errorsExt) ConvertToStandard() errors {
	if len(e) == 0 {
		return nil
	}

	standardError := make(errors, len(e))
	for i := range e[0].Message {
		standardError[i] = errorStruct{
			Message:   fmt.Sprintf("%v", e[0].Message[i]),
			Locations: e[0].Locations,
		}
	}

	return standardError
}

type operationType uint8

const (
	queryOperation operationType = iota
	mutationOperation
	//subscriptionOperation // Unused.
)
