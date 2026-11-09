package checkslip

import (
	"context"
	"errors"
)

// Iter walks every item of a list across pages, fetching the next page with
// next_cursor as it goes. Get one from a ListAll method:
//
//	it := client.Slips.ListAll(ctx, &checkslip.ListSlipsParams{})
//	for it.Next() {
//		rec := it.Current()
//		_ = rec
//	}
//	if err := it.Err(); err != nil {
//		return err
//	}
//
// With Go 1.23 or later you can also range over Iter.All.
//
// An Iter is not safe for concurrent use.
type Iter[T any] struct {
	ctx    context.Context
	fetch  func(ctx context.Context, cursor string) (*Page[T], error)
	cursor string
	items  []T
	cur    T
	err    error
	last   bool
}

func newIter[T any](ctx context.Context, cursor string, fetch func(context.Context, string) (*Page[T], error)) *Iter[T] {
	return &Iter[T]{ctx: ctx, fetch: fetch, cursor: cursor}
}

var errCursorLoop = errors.New("checkslip: the API returned the same cursor twice; stopping")

// Next moves to the next item and reports whether there is one. It fetches
// a new page when the current one is used up. When Next returns false,
// check Err.
func (it *Iter[T]) Next() bool {
	for len(it.items) == 0 {
		if it.err != nil || it.last {
			return false
		}
		page, err := it.fetch(it.ctx, it.cursor)
		if err != nil {
			it.err = err
			return false
		}
		it.items = page.Items
		switch {
		case page.NextCursor == "":
			it.last = true
		case page.NextCursor == it.cursor:
			it.err = errCursorLoop
			it.last = true
		default:
			it.cursor = page.NextCursor
		}
	}
	it.cur = it.items[0]
	var zero T
	it.items[0] = zero
	it.items = it.items[1:]
	return true
}

// Current returns the item Next moved to.
func (it *Iter[T]) Current() T { return it.cur }

// Err returns the error that stopped the iteration, or nil when the list
// ended normally.
func (it *Iter[T]) Err() error { return it.err }

// Cursor returns the cursor of the page after the one being read. Save it
// to resume a long walk later with ListOptions.Cursor. It is empty once the
// last page has been fetched.
func (it *Iter[T]) Cursor() string {
	if it.last {
		return ""
	}
	return it.cursor
}
