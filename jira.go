package gojira

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io/ioutil"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Jira struct {
	BaseUrl      string
	ApiPath      string
	ActivityPath string
	Client       *http.Client
	Auth         *Auth
}

type Auth struct {
	Login    string
	Password string
}

type Pagination struct {
	Total      int
	StartAt    int
	MaxResults int
	Page       int
	PageCount  int
	Pages      []int
}

func (p *Pagination) Compute() {
	p.PageCount = int(math.Ceil(float64(p.Total) / float64(p.MaxResults)))
	p.Page = int(math.Ceil(float64(p.StartAt) / float64(p.MaxResults)))

	p.Pages = make([]int, p.PageCount)
	for i := range p.Pages {
		p.Pages[i] = i
	}
}

type Issue struct {
	Id        string
	Key       string
	Self      string
	Expand    string
	Fields    *IssueFields
	CreatedAt time.Time
}

type IssueList struct {
	Expand     string
	StartAt    int
	MaxResults int
	Total      int
	Issues     []*Issue
	Pagination *Pagination
}

type IssueFields struct {
	IssueType        *IssueType
	Summary          string
	Description      string
	Status           *IssueStatus
	Comment          *IssueComment
	Reporter         *User
	Assignee         *User
	Sponsor          *User        `json:"customfield_10300"`
	CodeReviewer     *User        `json:"customfield_10202"`
	PrimaryDeveloper *User        `json:"customfield_10203"`
	QAReviewer       *User        `json:"customfield_12200"`
	ReleaseManager   *User        `json:"customfield_12300"`
	Comopnents       []*Component `json:"components"`
	IssueLinks       []*IssueLink `json:"issuelinks"`
	Project          *JiraProject
	Created          string
}

type IssueLink struct {
	Self         string     `json:"self"`
	Type         *IssueType `json:"type"`
	InwardIssue  *Issue     `json:"inwardIssue"`
	OutwardIssue *Issue     `json:"outwardIssue"`
}

type Component struct {
	Name string `json:"name"`
}

type IssueType struct {
	Self        string
	Id          string
	Description string
	IconUrl     string
	Name        string
	Subtask     bool
}

type IssueStatus struct {
	Description string
	Name        string
}

type IssueComment struct {
	Comments []Comment
}

type Comment struct {
	Author  *User `json:"author"`
	Body    string
	Created string
}

type JiraProject struct {
	Self       string
	Id         string
	Key        string
	Name       string
	AvatarUrls map[string]string
}

type ActivityItem struct {
	Title    string    `xml:"title"json:"title"`
	Id       string    `xml:"id"json:"id"`
	Link     []Link    `xml:"link"json:"link"`
	Updated  time.Time `xml:"updated"json:"updated"`
	Author   Person    `xml:"author"json:"author"`
	Summary  Text      `xml:"summary"json:"summary"`
	Category Category  `xml:"category"json:"category"`
}

type ActivityFeed struct {
	XMLName xml.Name        `xml:"http://www.w3.org/2005/Atom feed"json:"xml_name"`
	Title   string          `xml:"title"json:"title"`
	Id      string          `xml:"id"json:"id"`
	Link    []Link          `xml:"link"json:"link"`
	Updated time.Time       `xml:"updated,attr"json:"updated"`
	Author  Person          `xml:"author"json:"author"`
	Entries []*ActivityItem `xml:"entry"json:"entries"`
}

type Category struct {
	Term string `xml:"term,attr"json:"term"`
}

type Link struct {
	Rel  string `xml:"rel,attr,omitempty"json:"rel"`
	Href string `xml:"href,attr"json:"href"`
}

type Person struct {
	Name     string `xml:"name"json:"name"`
	URI      string `xml:"uri"json:"uri"`
	Email    string `xml:"email"json:"email"`
	InnerXML string `xml:",innerxml"json:"inner_xml"`
}

type Text struct {
	Type string `xml:"type,attr,omitempty"json:"type"`
	Body string `xml:",chardata"json:"body"`
}

type Params map[string]string

func (p Params) Query() string {
	var buffer bytes.Buffer

	for k, v := range p {
		buffer.WriteString(k)
		buffer.WriteString("=")
		buffer.WriteString(url.QueryEscape(v))
		buffer.WriteString("&")
	}

	return strings.TrimRight(buffer.String(), "&")
}

type ErrorResponse struct {
	Messages   []string          `json:"errorMessages"`
	Errors     map[string]string `json:"errors"`
	Status     string
	StatusCode int
}

func (e *ErrorResponse) String() string {
	if len(e.Messages) > 0 {
		message := e.Messages[0]
		return e.Status + ": " + message
	}

	return e.Status
}

func NewJira(baseUrl string, apiPath string, activityPath string, auth *Auth) *Jira {

	client := &http.Client{}

	return &Jira{
		BaseUrl:      baseUrl,
		ApiPath:      apiPath,
		ActivityPath: activityPath,
		Client:       client,
		Auth:         auth,
	}
}

const (
	dateLayout = "2006-01-02T15:04:05.000-0700"
)

func okStatus(code int) bool {
	switch {
	case 200 <= code && code < 300:
		return true
	}

	return false
}

func (j *Jira) buildAndExecRequest(method string, url string) (contents []byte, err error) {

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		err = errors.New("Error while building jira request")
		return
	}
	req.SetBasicAuth(j.Auth.Login, j.Auth.Password)

	resp, err := j.Client.Do(req)
	defer resp.Body.Close()
	contents, err = ioutil.ReadAll(resp.Body)

	if !okStatus(resp.StatusCode) {
		errResponse := new(ErrorResponse)
		err = json.Unmarshal(contents, &errResponse)
		errResponse.Status = resp.Status
		errResponse.StatusCode = resp.StatusCode

		if err != nil {
			return
		}

		err = errors.New(errResponse.String())
		return
	}

	return
}

func (j *Jira) UserActivity(user string) (ActivityFeed, error) {
	url := j.BaseUrl + j.ActivityPath + "?streams=" + url.QueryEscape("user IS "+user)

	return j.Activity(url)
}

func (j *Jira) Activity(url string) (activity ActivityFeed, err error) {
	contents, err := j.buildAndExecRequest("GET", url)
	if err != nil {
		return
	}

	err = xml.Unmarshal(contents, &activity)
	return
}

// search issues assigned to given user
func (j *Jira) IssuesAssignedTo(user string, maxResults int, startAt int) (issues IssueList, err error) {

	url := j.BaseUrl + j.ApiPath + "/search?jql=assignee=\"" + url.QueryEscape(user) + "\"&startAt=" + strconv.Itoa(startAt) + "&maxResults=" + strconv.Itoa(maxResults)
	contents, err := j.buildAndExecRequest("GET", url)
	if err != nil {
		return
	}

	err = json.Unmarshal(contents, &issues)
	if err != nil {
		return
	}

	for _, issue := range issues.Issues {
		t, _ := time.Parse(dateLayout, issue.Fields.Created)
		issue.CreatedAt = t
	}

	pagination := Pagination{
		Total:      issues.Total,
		StartAt:    issues.StartAt,
		MaxResults: issues.MaxResults,
	}
	pagination.Compute()

	issues.Pagination = &pagination

	return
}

// search an issue by its id
func (j *Jira) Issue(id string, params Params) (issue *Issue, err error) {

	url := j.BaseUrl + j.ApiPath + "/issue/" + id

	if params != nil {
		url += "?" + params.Query()
	}

	contents, err := j.buildAndExecRequest("GET", url)
	if err != nil {
		return
	}

	err = json.Unmarshal(contents, &issue)
	return
}
