package eaeunion

import (
	"context"
	"errors"
	"fmt"
)

// WalkOptions bounds a sequential traversal. Zero limits mean no limit.
type WalkOptions struct {
	PageSize   int
	MaxPages   int
	MaxRecords int
}

// WalkResult identifies the next offset after fully accepted pages.
type WalkResult struct {
	NextSkip int
	Pages    int
	Records  int
}

// Walk calls visit for each page. Return false to stop before accepting that
// page. On error, NextSkip is the first offset that was not accepted.
// A stable sort with a unique tie breaker is needed for reproducible results.
func (c *Client) Walk(ctx context.Context, collection string, query Query, options WalkOptions, visit func(Page) (bool, error)) (WalkResult, error) {
	if ctx == nil {
		return WalkResult{}, errors.New("context is required")
	}
	if visit == nil {
		return WalkResult{}, errors.New("visit callback is required")
	}
	if options.PageSize < 0 || options.MaxPages < 0 || options.MaxRecords < 0 {
		return WalkResult{}, errors.New("walk limits cannot be negative")
	}
	if options.PageSize == 0 {
		options.PageSize = defaultPageLimit
	}
	maxSize := 500
	if c != nil && c.mode != Anonymous {
		maxSize = 1000
	}
	if options.PageSize > maxSize {
		return WalkResult{}, fmt.Errorf("page size exceeds conservative mode limit of %d", maxSize)
	}
	result := WalkResult{}
	if query.Skip != nil {
		result.NextSkip = *query.Skip
	}
	if result.NextSkip < 0 {
		return result, errors.New("skip cannot be negative")
	}
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if options.MaxPages > 0 && result.Pages >= options.MaxPages {
			return result, nil
		}
		if options.MaxRecords > 0 && result.Records >= options.MaxRecords {
			return result, nil
		}
		size := options.PageSize
		if options.MaxRecords > 0 && options.MaxRecords-result.Records < size {
			size = options.MaxRecords - result.Records
		}
		current := query
		current.Limit = &size
		current.Skip = &result.NextSkip
		page, err := c.Find(ctx, collection, current)
		if err != nil {
			return result, err
		}
		if len(page.Result) == 0 {
			return result, nil
		}
		accepted, err := visit(page)
		if err != nil {
			return result, err
		}
		if !accepted {
			return result, nil
		}
		result.Pages++
		result.Records += len(page.Result)
		result.NextSkip += len(page.Result)
		if len(page.Result) < size {
			return result, nil
		}
		if page.MatchedDocuments != nil && int64(result.NextSkip) >= *page.MatchedDocuments {
			return result, nil
		}
	}
}
