package conformity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	eaeunion "github.com/ReanSn0w/go-eaeunion-api"
)

// Filter combines named conditions and Extra with $and. NameContains escapes
// regexp metacharacters; NameRegex is passed through deliberately.
type Filter struct {
	AuthorityID         string
	Country             string
	NameContains        string
	NameRegex           string
	RecordStatus        string
	AccreditationStatus string
	Extra               any
}

var ErrNotFound = errors.New("conformity body not found")
var ErrAmbiguous = errors.New("multiple conformity bodies match identifier")

// GetByID optionally narrows the identifier with a country code. It never
// silently selects one record when the identifier is ambiguous.
func (c *Client) GetByID(ctx context.Context, id, country string) (Record, error) {
	if id == "" {
		return Record{}, errors.New("authority ID is required")
	}
	limit := 2
	page, err := c.Find(ctx, Filter{AuthorityID: id, Country: country}, eaeunion.Query{Limit: &limit})
	if err != nil {
		return Record{}, err
	}
	if len(page.Records) == 0 {
		return Record{}, ErrNotFound
	}
	if len(page.Records) > 1 || (page.MatchedDocuments != nil && *page.MatchedDocuments > 1) {
		return Record{}, ErrAmbiguous
	}
	return page.Records[0], nil
}

func (f Filter) encode() (any, error) {
	conditions := make([]any, 0, 7)
	if f.AuthorityID != "" {
		conditions = append(conditions, map[string]any{"conformityAuthorityId": f.AuthorityID})
	}
	if f.Country != "" {
		conditions = append(conditions, map[string]any{"unifiedCountryCode.value": f.Country})
	}
	if f.NameContains != "" {
		conditions = append(conditions, map[string]any{"conformityAssessmentBodyDetails.businessEntityUnitName": map[string]any{"$regex": regexp.QuoteMeta(f.NameContains)}})
	}
	if f.NameRegex != "" {
		conditions = append(conditions, map[string]any{"conformityAssessmentBodyDetails.businessEntityUnitName": map[string]any{"$regex": f.NameRegex}})
	}
	if f.RecordStatus != "" {
		conditions = append(conditions, map[string]any{"statusV2Details.statusCode.value": f.RecordStatus})
	}
	if f.AccreditationStatus != "" {
		conditions = append(conditions, map[string]any{"accreditationCertificateDetails.statusV2Details.statusCode.value": f.AccreditationStatus})
	}
	if f.Extra != nil {
		if err := validateObject(f.Extra); err != nil {
			return nil, fmt.Errorf("extra filter: %w", err)
		}
		conditions = append(conditions, f.Extra)
	}
	if len(conditions) == 0 {
		return map[string]any{}, nil
	}
	if len(conditions) == 1 {
		return conditions[0], nil
	}
	return map[string]any{"$and": conditions}, nil
}

func combine(left, right any) (any, error) {
	if err := validateObject(right); err != nil {
		return nil, fmt.Errorf("query filter: %w", err)
	}
	return map[string]any{"$and": []any{left, right}}, nil
}
func validateObject(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = bytes.TrimSpace(b)
	if len(b) == 0 || b[0] != '{' || !json.Valid(b) {
		return errors.New("filter must be a JSON object")
	}
	return nil
}
