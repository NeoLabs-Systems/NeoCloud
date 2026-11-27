package webdav

import (
	"encoding/xml"
	"io"
	"net/http"
	"strconv"
	"strings"
)

const (
	DAV   = "DAV:"
	OC    = "http://owncloud.org/ns"
	NC    = "http://nextcloud.org/ns"
	Sabre = "http://sabredav.org/ns"
)

const (
	MaxXMLBodyBytes = 1 << 20
	maxXMLDepth     = 64
)

func badXML(message string) error {
	return HTTPError{Status: http.StatusBadRequest, Message: message}
}

// ReadBody returns a request's XML body, refusing more than MaxXMLBodyBytes
// (unread when the client declares its length).
func ReadBody(r *http.Request) ([]byte, error) {
	tooLarge := HTTPError{Status: http.StatusRequestEntityTooLarge, Message: "The XML body is too large."}
	if r.ContentLength > MaxXMLBodyBytes {
		return nil, tooLarge
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, MaxXMLBodyBytes+1))
	if err != nil {
		return nil, badXML("The request body could not be read.")
	}
	if len(data) > MaxXMLBodyBytes {
		return nil, tooLarge
	}
	return data, nil
}

type XMLNode struct {
	Name, Namespace, Text string
	Children              []*XMLNode
}

// ParseXML reads elements and text. DOCTYPE and entity declarations are
// refused outright, so there is no entity expansion to abuse.
func ParseXML(data []byte) (*XMLNode, error) {
	const invalid = "The request body is not valid XML."
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	var stack []*XMLNode
	var root *XMLNode
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, badXML(invalid)
		}
		switch t := token.(type) {
		case xml.Directive:
			return nil, badXML("DOCTYPE and entity declarations are not accepted.")
		case xml.StartElement:
			node := &XMLNode{Name: t.Name.Local, Namespace: t.Name.Space}
			switch {
			case len(stack) >= maxXMLDepth || len(stack) == 0 && root != nil:
				return nil, badXML(invalid)
			case len(stack) == 0:
				root = node
			default:
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, node)
			}
			stack = append(stack, node)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].Text += string(t)
			}
		}
	}
	if root == nil || len(stack) != 0 {
		return nil, badXML(invalid)
	}
	return root, nil
}

func (n *XMLNode) Is(namespace, name string) bool {
	return n != nil && n.Namespace == namespace && n.Name == name
}

func (n *XMLNode) Child(namespace, name string) *XMLNode {
	if n == nil {
		return nil
	}
	for _, child := range n.Children {
		if child.Is(namespace, name) {
			return child
		}
	}
	return nil
}

// InnerText is all character data below n.
func (n *XMLNode) InnerText() string {
	if n == nil {
		return ""
	}
	var text strings.Builder
	text.WriteString(n.Text)
	for _, child := range n.Children {
		text.WriteString(child.InnerText())
	}
	return strings.TrimSpace(text.String())
}

var escaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

func Escape(value string) string { return escaper.Replace(value) }

var prefixes = map[string]string{DAV: "d", OC: "oc", NC: "nc", Sabre: "s"}

// Element renders one element with already escaped content. Namespaces other
// than the four declared on the root are declared in place, so any property a
// client asked for can be echoed.
func Element(namespace, name, content string) string {
	tag, declaration := prefixes[namespace]+":"+name, ""
	switch {
	case namespace == "":
		tag, declaration = name, ` xmlns=""`
	case prefixes[namespace] == "":
		tag, declaration = "x:"+name, ` xmlns:x="`+Escape(namespace)+`"`
	}
	if content == "" {
		return "<" + tag + declaration + "/>"
	}
	return "<" + tag + declaration + ">" + content + "</" + tag + ">"
}

// Document wraps content in a DAV: root element declaring the common namespaces.
func Document(name, content string) string {
	return `<?xml version="1.0" encoding="utf-8"?>` + "\n<d:" + name +
		` xmlns:d="DAV:" xmlns:oc="` + OC + `" xmlns:nc="` + NC + `" xmlns:s="` + Sabre + `">` + content + "</d:" + name + ">\n"
}

func WriteXML(w http.ResponseWriter, status int, document string) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(document)))
	w.WriteHeader(status)
	_, _ = io.WriteString(w, document)
}
