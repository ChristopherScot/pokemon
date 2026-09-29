package api

import (
	"context"
	"iter"
)

// Paged iterates every item across pages of a cursor-based operation.
// The caller closes over the generated client method so one
// implementation covers every paged operation.
//
//	for user, err := range Paged(ctx, func(ctx context.Context, cursor string) (Page[User], error) {
//	    res, err := c.ListUsers(ctx, ListUsersParams{After: cursor})
//	    if err != nil {
//	        return Page[User]{}, err
//	    }
//	    return Page[User]{Items: res.Items, Next: res.NextCursor}, nil
//	}) {
//	    if err != nil {
//	        return err
//	    }
//	    // use user
//	}

// Page is one response from a paged operation. An empty Next ends
// iteration.
type Page[T any] struct {
	Items []T
	Next  string
}

// Fetch requests one page. cursor is empty on the first call.
type Fetch[T any] func(ctx context.Context, cursor string) (Page[T], error)

// Paged yields an error exactly once (as its final element) and
// stops, so a range loop that checks err and returns cannot silently
// process a truncated list. Breaking the loop stops fetching.
func Paged[T any](ctx context.Context, fetch Fetch[T]) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		var cursor string
		seen := map[string]bool{}

		for {
			if err := ctx.Err(); err != nil {
				var zero T
				yield(zero, err)
				return
			}

			page, err := fetch(ctx, cursor)
			if err != nil {
				var zero T
				yield(zero, err)
				return
			}

			for _, item := range page.Items {
				if !yield(item, nil) {
					return
				}
			}

			// Stop on a repeated cursor: a server that loops one would
			// otherwise spin forever.
			if page.Next == "" || seen[page.Next] {
				return
			}
			seen[page.Next] = true
			cursor = page.Next
		}
	}
}
