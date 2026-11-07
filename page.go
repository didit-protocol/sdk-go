package didit

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/didit-protocol/sdk-go/option"
)

// Page is one page of a list: the fields of the API's
// {count, next, previous, results} envelope. List methods return it, and
// their AutoPaging variants iterate over every item of every page.
//
// Lists that send more top-level fields (for example kpis or
// total_transactions) keep them readable through JSON.Field.
type Page[T any] struct {
	// Count is the total number of items, on every page.
	Count int64 `json:"count"`
	// Next is the URL of the next page, empty on the last page.
	Next string `json:"next"`
	// Previous is the URL of the previous page, empty on the first page.
	Previous string `json:"previous"`
	// Results holds the items of this page.
	Results []T `json:"results"`

	JSON ResponseJSON `json:"-"`

	client  *Client
	request *apiRequest
	opts    []option.RequestOption
	url     *url.URL
	fetched *pageHistory
}

// pageHistory lists the pages fetched up to and including one page, newest
// first. Every page shares its predecessors' entries, so the list costs one
// entry per page.
type pageHistory struct {
	key    string
	parent *pageHistory
}

func (h *pageHistory) contains(key string) bool {
	for ; h != nil; h = h.parent {
		if h.key == key {
			return true
		}
	}
	return false
}

// pageKey identifies the page a URL points to: scheme and host compared
// case-insensitively with the default port made explicit, the fragment
// dropped and the query parameters sorted.
func pageKey(u *url.URL) string {
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	return strings.ToLower(u.Scheme) + "://" + canonicalHost(u) + path + "?" + u.Query().Encode()
}

// UnmarshalJSON decodes a page leniently, like every response type.
func (p *Page[T]) UnmarshalJSON(data []byte) error {
	return unmarshalResponse(data, p, &p.JSON)
}

// HasNextPage reports whether the API announced a next page.
func (p *Page[T]) HasNextPage() bool {
	return p != nil && strings.TrimSpace(p.Next) != ""
}

// GetNextPage fetches the next page. It returns nil and no error after the
// last page. A next link on an origin other than the configured base URL is
// refused with an *UnexpectedResponseError, so the API key never leaves it,
// and so is a next link back to this page or one fetched before it, which
// would loop forever.
func (p *Page[T]) GetNextPage(ctx context.Context) (*Page[T], error) {
	if !p.HasNextPage() || p.client == nil {
		return nil, nil
	}
	cfg, err := p.client.config(p.opts)
	if err != nil {
		return nil, err
	}
	next, err := p.url.Parse(strings.TrimSpace(p.Next))
	if err != nil {
		return nil, &UnexpectedResponseError{Message: fmt.Sprintf("invalid next page link: %v", err)}
	}
	base := cfg.BaseURL
	if p.request.api == apiAccount {
		base = cfg.AccountBaseURL
	}
	if !sameOrigin(next, base) {
		return nil, &UnexpectedResponseError{Message: fmt.Sprintf("refusing a next page link on another origin (%s://%s)", next.Scheme, next.Host)}
	}
	if p.fetched.contains(pageKey(next)) {
		return nil, &UnexpectedResponseError{Message: fmt.Sprintf("refusing a next page link to a page already fetched (%s)", next.EscapedPath())}
	}
	req := *p.request
	req.rawURL = next
	return fetchPage[T](ctx, p.client, &req, p.opts, p.fetched)
}

// getPage fetches the first page of a list.
func getPage[T any](ctx context.Context, c *Client, r *apiRequest, opts []option.RequestOption) (*Page[T], error) {
	return fetchPage[T](ctx, c, r, opts, nil)
}

// fetchPage fetches one page and records it after the pages in fetched. A
// next page reached through a redirect is recorded under both its link and
// the URL it was served from.
func fetchPage[T any](ctx context.Context, c *Client, r *apiRequest, opts []option.RequestOption, fetched *pageHistory) (*Page[T], error) {
	page := &Page[T]{}
	resp, err := c.do(ctx, r, page, opts)
	if err != nil {
		return nil, err
	}
	if r.rawURL != nil {
		fetched = &pageHistory{key: pageKey(r.rawURL), parent: fetched}
	}
	if key := pageKey(resp.Request.URL); fetched == nil || fetched.key != key {
		fetched = &pageHistory{key: key, parent: fetched}
	}
	page.client = c
	page.request = r
	page.opts = opts
	page.url = resp.Request.URL
	page.fetched = fetched
	return page, nil
}

// PageIterator iterates over the items of every page of a list, fetching the
// next page when needed:
//
//	iter := client.Sessions.ListAutoPaging(ctx, didit.SessionsListParams{})
//	for iter.Next() {
//		session := iter.Current()
//		// ...
//	}
//	if err := iter.Err(); err != nil {
//		// ...
//	}
//
// With Go 1.23 or later, All returns an iter.Seq2 for a range loop.
type PageIterator[T any] struct {
	ctx   context.Context
	first func() (*Page[T], error)
	page  *Page[T]
	index int
	cur   T
	err   error
	done  bool
}

func newPageIterator[T any](ctx context.Context, first func() (*Page[T], error)) *PageIterator[T] {
	return &PageIterator[T]{ctx: ctx, first: first, index: -1}
}

// Next advances to the next item, fetching pages as needed. It returns false
// at the end or on an error; check Err afterwards.
func (it *PageIterator[T]) Next() bool {
	if it.done {
		return false
	}
	if it.page == nil {
		it.page, it.err = it.first()
		if it.err != nil || it.page == nil {
			it.done = true
			return false
		}
	}
	for {
		it.index++
		if it.index < len(it.page.Results) {
			it.cur = it.page.Results[it.index]
			return true
		}
		next, err := it.page.GetNextPage(it.ctx)
		if err != nil {
			it.err = err
			it.done = true
			return false
		}
		if next == nil {
			it.done = true
			return false
		}
		it.page = next
		it.index = -1
	}
}

// Current returns the current item.
func (it *PageIterator[T]) Current() T { return it.cur }

// Err returns the error that stopped the iteration, if any.
func (it *PageIterator[T]) Err() error { return it.err }

// Page returns the page holding the current item.
func (it *PageIterator[T]) Page() *Page[T] { return it.page }
