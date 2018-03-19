package sugar

import (
	"net/http"
	"strconv"
	"encoding/json"
	"bytes"
	"io/ioutil"
	"strings"
	"reflect"
	"net/url"
	"mime/multipart"
	"os"
	"io"
	"encoding/xml"
)

var (
	Encode = ToString
)

type List []interface{}

type L = List

type Map map[string]interface{}

type M = Map

type Header Map

type H = Header

type Path Map

type P = Path

type Query Map

type Q = Query

type Form Map

type F = Form

type Json struct {
	Payload interface{}
}

type J = Json

type Cookie Map

type C = Cookie

type User struct {
	Name, Password string
}

type U = User

type MultiPart Map

type MP = MultiPart

type Xml struct {
	Payload interface{}
}

type Mapper struct {
	mapper func(*http.Request)
}

type RequestContext struct {
	Request    *http.Request
	Response   *http.Response
	Params     []interface{}
	Param      interface{}
	ParamIndex int
}

type Encoder interface {
	Encode(context *RequestContext, chain *EncoderChain) error
}

type EncoderChain struct {
	encoders []Encoder
	index    int
}

func (c *EncoderChain) Next(context *RequestContext) error {
	if c.index < len(c.encoders) {
		c.index++
		return c.encoders[c.index-1].Encode(context, c)
	}
	return EncoderNotFound
}

func (c *EncoderChain) Reset() *EncoderChain {
	c.encoders = []Encoder{}
	c.index = 0
	return c
}

func (c *EncoderChain) Add(Encoders ... Encoder) *EncoderChain {
	for _, Encoder := range Encoders {
		c.encoders = append(c.encoders, Encoder)
	}
	return c
}

func (c *EncoderChain) First() Encoder {
	if len(c.encoders) > 0 {
		return c.encoders[0]
	}
	return nil
}

func NewEncoderChain(encoders ... Encoder) *EncoderChain {
	chain := &EncoderChain{}
	chain.Reset()
	chain.Add(encoders...)
	return chain
}

type PathEncoder struct {
}

func (r *PathEncoder) Encode(context *RequestContext, chain *EncoderChain) error {
	pathParams, ok := context.Param.(Path)
	if !ok {
		return chain.Next(context)
	}

	req := context.Request
	for i := 0; i < len(req.URL.Path); i++ {
		if string(req.URL.Path[i]) == ":" {
			j := i + 1
			for ; j < len(req.URL.Path); j++ {
				s := string(req.URL.Path[j])
				if s == "/" {
					break
				}
			}

			key := req.URL.Path[i+1: j]
			value := pathParams[key]
			req.URL.Path = strings.Replace(req.URL.Path, req.URL.Path[i:j], Encode(value), -1)
		}
	}
	return nil
}

type QueryEncoder struct {
}

func (r *QueryEncoder) Encode(context *RequestContext, chain *EncoderChain) error {
	queryParams, ok := context.Param.(Query)
	if !ok {
		return chain.Next(context)
	}

	req := context.Request
	q := req.URL.Query()
	for k, v := range queryParams {
		switch reflect.TypeOf(v).Kind() {
		case reflect.Array, reflect.Slice:
			foreach(v, func(i interface{}) {
				q.Add(k, Encode(i))
			})
		default:
			q.Add(k, Encode(v))
		}
	}
	req.URL.RawQuery = q.Encode()
	return nil
}

type HeaderEncoder struct {
}

func (r *HeaderEncoder) Encode(context *RequestContext, chain *EncoderChain) error {
	headerParams, ok := context.Param.(Header)
	if !ok {
		return chain.Next(context)
	}

	for k, v := range headerParams {
		context.Request.Header.Add(k, Encode(v))
	}
	return nil
}

type FormEncoder struct {
}

func (r *FormEncoder) Encode(context *RequestContext, chain *EncoderChain) error {
	formParams, ok := context.Param.(Form)
	if !ok {
		return chain.Next(context)
	}

	form := url.Values{}
	for k, v := range formParams {
		switch reflect.TypeOf(v).Kind() {
		case reflect.Array, reflect.Slice:
			foreach(v, func(i interface{}) {
				form.Add(k, Encode(i))
			})
		default:
			form.Add(k, Encode(v))
		}
	}

	req := context.Request
	req.PostForm = form
	err := req.ParseForm()
	if err != nil {
		return err
	}

	if _, ok := req.Header[ContentType]; !ok {
		req.Header.Set(ContentType, ContentTypeForm)
	}
	return nil
}

type JsonEncoder struct {
}

func (r *JsonEncoder) Encode(context *RequestContext, chain *EncoderChain) error {
	jsonParams, ok := context.Param.(Json)
	if !ok {
		return chain.Next(context)
	}

	var b []byte
	var err error
	switch x := jsonParams.Payload.(type) {
	case []byte:
		b, err = json.RawMessage(x).MarshalJSON()
	case string:
		b, err = json.RawMessage([]byte(x)).MarshalJSON()
	default:
		b, err = json.Marshal(x)
	}

	if err != nil {
		return err
	}

	req := context.Request
	req.Body = ioutil.NopCloser(bytes.NewReader(b))

	if _, ok := req.Header[ContentType]; !ok {
		req.Header.Set(ContentType, ContentTypeJsonUtf8)
	}
	return nil
}

type CookieEncoder struct {
}

func (r *CookieEncoder) Encode(context *RequestContext, chain *EncoderChain) error {
	cookieParams, ok := context.Param.(Cookie)
	if !ok {
		return chain.Next(context)
	}

	for k, v := range cookieParams {
		context.Request.AddCookie(&http.Cookie{Name: k, Value: Encode(v)})
	}
	return nil
}

type BasicAuthEncoder struct {
}

func (r *BasicAuthEncoder) Encode(context *RequestContext, chain *EncoderChain) error {
	authParams, ok := context.Param.(User)
	if !ok {
		return chain.Next(context)
	}

	context.Request.SetBasicAuth(authParams.Name, authParams.Password)
	return nil
}

type MultiPartEncoder struct {
}

func (r *MultiPartEncoder) Encode(context *RequestContext, chain *EncoderChain) error {
	multiPartParams, ok := context.Param.(MultiPart)
	if !ok {
		return chain.Next(context)
	}

	b := &bytes.Buffer{}
	w := multipart.NewWriter(b)
	defer w.Close()
	for k, v := range multiPartParams {
		switch x := v.(type) {
		case *os.File:
			if err := writeFile(w, k, x.Name(), x); err != nil {
				return err
			}
		default:
			w.WriteField(k, Encode(v))
		}
	}

	req := context.Request
	req.Body = ioutil.NopCloser(b)

	if _, ok := req.Header[ContentType]; !ok {
		req.Header.Set(ContentType, w.FormDataContentType())
	}
	return nil
}

func writeFile(w *multipart.Writer, fieldName, fileName string, file io.Reader) error {
	fileWriter, err := w.CreateFormFile(fieldName, fileName)
	if err != nil {
		return err
	}

	if _, err = io.Copy(fileWriter, file); err != nil {
		return err
	}

	return nil
}

type PlainTextEncoder struct {
}

func (r *PlainTextEncoder) Encode(context *RequestContext, chain *EncoderChain) error {
	textParams, ok := context.Param.(string)
	if !ok {
		return chain.Next(context)
	}

	b := &bytes.Buffer{}
	b.WriteString(textParams)
	req := context.Request
	req.Body = ioutil.NopCloser(b)

	if _, ok := req.Header[ContentType]; !ok {
		req.Header.Set(ContentType, ContentTypePlainText)
	}
	return nil
}

type XmlEncoder struct {
}

func (r *XmlEncoder) Encode(context *RequestContext, chain *EncoderChain) error {
	xmlParams, ok := context.Param.(Xml)
	if !ok {
		return chain.Next(context)
	}

	var b []byte
	var err error
	switch x := xmlParams.Payload.(type) {
	case string:
		b = []byte(x)
	default:
		b, err = xml.Marshal(x)
	}

	if err != nil {
		return err
	}

	req := context.Request
	req.Body = ioutil.NopCloser(bytes.NewReader(b))

	if _, ok := req.Header[ContentType]; !ok {
		req.Header.Set(ContentType, ContentTypeXmlUtf8)
	}
	return nil
}

type MapperEncoder struct {
}

func (r *MapperEncoder) Encode(context *RequestContext, chain *EncoderChain) error {
	mapperParams, ok := context.Param.(Mapper)
	if !ok {
		return chain.Next(context)
	}

	mapperParams.mapper(context.Request)
	return nil
}

func ToString(v interface{}) string {
	var s string
	switch x := v.(type) {
	case bool:
		s = strconv.FormatBool(x)
	case uint:
		s = strconv.FormatUint(uint64(x), 10)
	case uint8:
		s = strconv.FormatUint(uint64(x), 10)
	case uint16:
		s = strconv.FormatUint(uint64(x), 10)
	case uint32:
		s = strconv.FormatUint(uint64(x), 10)
	case uint64:
		s = strconv.FormatUint(uint64(x), 10)
	case int:
		s = strconv.FormatInt(int64(x), 10)
	case int8:
		s = strconv.FormatInt(int64(x), 10)
	case int16:
		s = strconv.FormatInt(int64(x), 10)
	case int32:
		s = strconv.FormatInt(int64(x), 10)
	case int64:
		s = strconv.FormatInt(int64(x), 10)
	case float32:
		s = strconv.FormatFloat(float64(x), 'f', -1, 32)
	case float64:
		s = strconv.FormatFloat(float64(x), 'f', -1, 64)
	case string:
		s = v.(string)
	}
	return s
}

func foreach(v interface{}, f func(interface{})) {
	a := reflect.ValueOf(v)
	for i := 0; i < a.Len(); i++ {
		f(a.Index(i).Elem().Interface())
	}
}