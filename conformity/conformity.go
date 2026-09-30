// Package conformity provides typed access to the EAEU conformity assessment
// bodies registry. Transport and pagination are delegated to package eaeunion.
package conformity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	eaeunion "github.com/ReanSn0w/go-eaeunion-api"
)

const (
	Endpoint   = "https://tech.eaeunion.org/spd/find"
	Collection = "kbdread.service-prop-36-v_conformityAssessmentBodyInformationDetailsType_organization_table"
)

type Code struct {
	Value      string `json:"value"`
	CodeListID string `json:"codeListId,omitempty"`
}
type OID struct {
	Value string `json:"$oid"`
}
type Date struct {
	Value string `json:"$date"`
}
type Status struct {
	Code Code `json:"statusCode"`
}
type Address struct {
	Kind     string `json:"addressKindCode"`
	Country  Code   `json:"unifiedCountryCode"`
	Region   string `json:"regionName"`
	City     string `json:"cityName"`
	Street   string `json:"streetName"`
	Building string `json:"buildingNumberId"`
	Room     string `json:"roomNumberId"`
	PostCode string `json:"postCode"`
	Full     string `json:"address"`
}
type PersonName struct {
	First  string `json:"firstName"`
	Middle string `json:"middleName"`
	Last   string `json:"lastName"`
}
type Communication struct {
	ChannelCode string   `json:"communicationChannelCode"`
	IDs         []string `json:"communicationChannelId"`
}
type Officer struct {
	Name           PersonName      `json:"fullNameDetails"`
	FullName       string          `json:"fullName"`
	Position       string          `json:"positionName"`
	Communications []Communication `json:"communicationDetails"`
}
type BodyDetails struct {
	Name      string    `json:"businessEntityUnitName"`
	Addresses []Address `json:"addressV4Details,omitempty"`
	Officers  []Officer `json:"officerDetails,omitempty"`
}
type BusinessEntity struct {
	Name      string `json:"businessEntityName"`
	BriefName string `json:"businessEntityBriefName,omitempty"`
}
type Certificate struct {
	ID         string `json:"docId"`
	Created    Date   `json:"docCreationDate"`
	ValidUntil Date   `json:"docValidityDate"`
	Status     Status `json:"statusV2Details"`
}
type ConformityObject struct {
	Name            string            `json:"conformityObjectName"`
	CommodityCodes  []json.RawMessage `json:"commodityCode,omitempty"`
	Characteristics []json.RawMessage `json:"conformityObjectCharacteristicDetails,omitempty"`
}
type AccreditationArea struct {
	ProductKindCode       string             `json:"productKindCode"`
	TechnicalRegulationID string             `json:"technicalRegulationId"`
	Objects               []ConformityObject `json:"conformityObjectDetails,omitempty"`
	Text                  []json.RawMessage  `json:"accreditationAreaText,omitempty"`
}

// Record retains Raw to preserve fields not yet modeled by this package.
type Record struct {
	OID                OID                 `json:"_id"`
	Country            Code                `json:"unifiedCountryCode"`
	AuthorityID        string              `json:"conformityAuthorityId"`
	Body               BodyDetails         `json:"conformityAssessmentBodyDetails"`
	BusinessEntity     BusinessEntity      `json:"businessEntityDetails"`
	Certificate        Certificate         `json:"accreditationCertificateDetails"`
	RecordStatus       Status              `json:"statusV2Details"`
	AccreditationAreas []AccreditationArea `json:"accreditationAreaDetails,omitempty"`
	Organizations      []string            `json:"organizations,omitempty"`
	Raw                json.RawMessage     `json:"-"`
}

type Page struct {
	Records          []Record
	TotalDocuments   *int64
	MatchedDocuments *int64
	ResponseSize     int64
}

type Client struct{ core *eaeunion.Client }

func NewClient(core *eaeunion.Client) (*Client, error) {
	if core == nil {
		return nil, errors.New("eaeunion client is required")
	}
	return &Client{core: core}, nil
}

func NewPublicClient() (*Client, error) {
	core, err := eaeunion.NewClient(eaeunion.Config{Endpoint: Endpoint})
	if err != nil {
		return nil, err
	}
	return NewClient(core)
}

func (c *Client) Find(ctx context.Context, filter Filter, query eaeunion.Query) (Page, error) {
	if c == nil || c.core == nil {
		return Page{}, errors.New("nil conformity client")
	}
	combined, err := filter.encode()
	if err != nil {
		return Page{}, err
	}
	if query.Filter != nil {
		combined, err = combine(combined, query.Filter)
		if err != nil {
			return Page{}, err
		}
	}
	query.Filter = combined
	page, err := c.core.Find(ctx, Collection, query)
	if err != nil {
		return Page{}, err
	}
	return decode(page)
}

func (c *Client) Walk(ctx context.Context, filter Filter, query eaeunion.Query, options eaeunion.WalkOptions, visit func(Page) (bool, error)) (eaeunion.WalkResult, error) {
	if c == nil || c.core == nil {
		return eaeunion.WalkResult{}, errors.New("nil conformity client")
	}
	if visit == nil {
		return eaeunion.WalkResult{}, errors.New("visit callback is required")
	}
	combined, err := filter.encode()
	if err != nil {
		return eaeunion.WalkResult{}, err
	}
	if query.Filter != nil {
		combined, err = combine(combined, query.Filter)
		if err != nil {
			return eaeunion.WalkResult{}, err
		}
	}
	query.Filter = combined
	return c.core.Walk(ctx, Collection, query, options, func(raw eaeunion.Page) (bool, error) {
		page, err := decode(raw)
		if err != nil {
			return false, err
		}
		return visit(page)
	})
}

func decode(raw eaeunion.Page) (Page, error) {
	page := Page{Records: make([]Record, 0, len(raw.Result)), TotalDocuments: raw.TotalDocuments, MatchedDocuments: raw.MatchedDocuments, ResponseSize: raw.ResponseSize}
	for i, item := range raw.Result {
		var record Record
		if err := json.Unmarshal(item, &record); err != nil {
			return Page{}, fmt.Errorf("decode conformity record %d: %w", i, err)
		}
		record.Raw = append(json.RawMessage(nil), item...)
		page.Records = append(page.Records, record)
	}
	return page, nil
}
