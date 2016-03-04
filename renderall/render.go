package renderall

import (
	"bytes"
	"encoding/json"
	"html/template"
	"net/http"
)

const (
	// ContentBinary header value for binary data.
	ContentBinary = "application/octet-stream"
	// ContentHTML header value for HTML data.
	ContentHTML = "text/html"
	// ContentJSON header value for JSON data.
	ContentJSON = "application/json"
	// ContentJSONP header value for JSONP data.
	ContentJSONP = "application/javascript"
	// ContentLength header constant.
	ContentLength = "Content-Length"
	// ContentText header value for Text data.
	ContentText = "text/plain"
	// ContentType header constant.
	ContentType = "Content-Type"
	// ContentXHTML header value for XHTML data.
	ContentXHTML = "application/xhtml+xml"
	// ContentXML header value for XML data.
	ContentXML = "text/xml"
	// Default character encoding.
	defaultCharset = "UTF-8"
)

// Options is a struct for specifying configuration options for the render.Render object.
type Options struct {
	// Directory to load templates. Default is "templates".
	Directory string
	// Asset function to use in place of directory. Defaults to nil.
	Asset func(name string) ([]byte, error)
	// AssetNames function to use in place of directory. Defaults to nil.
	AssetNames func() []string
	// Layout template name. Will not render a layout if blank (""). Defaults to blank ("").
	Layout string
	// Extensions to parse template files from. Defaults to [".tmpl"].
	Extensions []string
	// Funcs is a slice of FuncMaps to apply to the template upon compilation. This is useful for helper functions. Defaults to [].
	Funcs []template.FuncMap
	// Delims sets the action delimiters to the specified strings in the Delims struct.
	//Delims Delims
	// Appends the given character set to the Content-Type header. Default is "UTF-8".
	Charset string
	// Outputs human readable JSON.
	IndentJSON bool
	// Outputs human readable XML. Default is false.
	IndentXML bool
	// Prefixes the JSON output with the given bytes. Default is false.
	PrefixJSON []byte
	// Prefixes the XML output with the given bytes.
	PrefixXML []byte
	// Allows changing of output to XHTML instead of HTML. Default is "text/html"
	HTMLContentType string
	// If IsDevelopment is set to true, this will recompile the templates on every request. Default is false.
	IsDevelopment bool
	// Unescape HTML characters "&<>" to their original values. Default is false.
	UnEscapeHTML bool
	// Streams JSON responses instead of marshalling prior to sending. Default is false.
	StreamingJSON bool
	// Require that all blocks executed in the layout are implemented in all templates using the layout. Default is false.
	RequireBlocks bool
	// Disables automatic rendering of http.StatusInternalServerError when an error occurs. Default is false.
	DisableHTTPErrorRendering bool
}

// HTMLOptions is a struct for overriding some rendering Options for specific HTML call.
type HTMLOptions struct {
	// Layout template name. Overrides Options.Layout.
	Layout string
}

// New constructs a new Render instance with the supplied options.
func New(options ...Options) *Render {
	var o Options
	if len(options) == 0 {
		o = Options{}
	} else {
		o = options[0]
	}

	r := Render{
		opt: o,
	}
	r.opt.Charset = defaultCharset
	r.prepareOptions()
	//r.compileTemplates()

	// Create a new buffer pool for writing templates into.
	if bufPool == nil {
		bufPool = NewBufferPool(64)
	}

	return &r
}

func (r *Render) prepareOptions() {
	// Fill in the defaults if need be.
	if len(r.opt.Charset) == 0 {
		r.opt.Charset = defaultCharset
	}
	r.compiledCharset = "; charset=" + r.opt.Charset

	if len(r.opt.Directory) == 0 {
		r.opt.Directory = "templates"
	}
	if len(r.opt.Extensions) == 0 {
		r.opt.Extensions = []string{".tmpl"}
	}
	if len(r.opt.HTMLContentType) == 0 {
		r.opt.HTMLContentType = ContentHTML
	}
}

// Render is a service that provides functions for easily writing JSON, XML,
// binary data, and HTML templates out to a HTTP Response.
type Render struct {
	// Customize Secure with an Options struct.
	opt             Options
	compiledCharset string
}

type Head struct {
	ContentType string
	Status      int
}

// Data built-in renderer.
type Data struct {
	Head
}

// Engine is the generic interface for all responses.
type Engine interface {
	Render(http.ResponseWriter, interface{}) error
}

// HTML built-in renderer.
type HTML struct {
	Head
	Name      string
	Templates *template.Template
}

// JSON built-in renderer.
type JSON struct {
	Head
	Indent        bool
	UnEscapeHTML  bool
	Prefix        []byte
	StreamingJSON bool
}

// JSONP built-in renderer.
type JSONP struct {
	Head
	Indent   bool
	Callback string
}

// Write outputs the header content.
func (h Head) Write(w http.ResponseWriter) {
	w.Header().Set(ContentType, h.ContentType)
	w.WriteHeader(h.Status)
}

// Render a data response.
func (d Data) Render(w http.ResponseWriter, v interface{}) error {
	c := w.Header().Get(ContentType)
	if c != "" {
		d.Head.ContentType = c
	}

	d.Head.Write(w)
	w.Write(v.([]byte))
	return nil
}

// Render a HTML response.
func (h HTML) Render(w http.ResponseWriter, binding interface{}) error {
	// Retrieve a buffer from the pool to write to.
	out := bufPool.Get()
	err := h.Templates.ExecuteTemplate(out, h.Name, binding)
	if err != nil {
		return err
	}

	h.Head.Write(w)
	out.WriteTo(w)

	// Return the buffer to the pool.
	bufPool.Put(out)
	return nil
}

// Render a JSON response.
func (j JSON) Render(w http.ResponseWriter, v interface{}) error {
	if j.StreamingJSON {
		return j.renderStreamingJSON(w, v)
	}

	var result []byte
	var err error

	if j.Indent {
		result, err = json.MarshalIndent(v, "", "  ")
		result = append(result, '\n')
	} else {
		result, err = json.Marshal(v)
	}
	if err != nil {
		return err
	}

	// Unescape HTML if needed.
	if j.UnEscapeHTML {
		result = bytes.Replace(result, []byte("\\u003c"), []byte("<"), -1)
		result = bytes.Replace(result, []byte("\\u003e"), []byte(">"), -1)
		result = bytes.Replace(result, []byte("\\u0026"), []byte("&"), -1)
	}

	// JSON marshaled fine, write out the result.
	j.Head.Write(w)
	if len(j.Prefix) > 0 {
		w.Write(j.Prefix)
	}
	w.Write(result)
	return nil
}

func (j JSON) renderStreamingJSON(w http.ResponseWriter, v interface{}) error {
	j.Head.Write(w)
	if len(j.Prefix) > 0 {
		w.Write(j.Prefix)
	}

	return json.NewEncoder(w).Encode(v)
}

// Render a JSONP response.
func (j JSONP) Render(w http.ResponseWriter, v interface{}) error {
	var result []byte
	var err error

	if j.Indent {
		result, err = json.MarshalIndent(v, "", "  ")
	} else {
		result, err = json.Marshal(v)
	}
	if err != nil {
		return err
	}

	// JSON marshaled fine, write out the result.
	j.Head.Write(w)
	w.Write([]byte(j.Callback + "("))
	w.Write(result)
	w.Write([]byte(");"))

	// If indenting, append a new line.
	if j.Indent {
		w.Write([]byte("\n"))
	}
	return nil
}

//engine
// Render is the generic function called by XML, JSON, Data, HTML, and can be called by custom implementations.
func (r *Render) Render(w http.ResponseWriter, e Engine, data interface{}) error {
	err := e.Render(w, data)
	if err != nil && !r.opt.DisableHTTPErrorRendering {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	return err
}

// Data writes out the raw bytes as binary data.
func (r *Render) Data(w http.ResponseWriter, status int, v []byte) error {
	head := Head{
		ContentType: ContentBinary,
		Status:      status,
	}

	d := Data{
		Head: head,
	}

	return r.Render(w, d, v)
}

// HTML builds up the response from the specified template and bindings.
// TODO: make work ;)
/*
func (r *Render) HTML(w http.ResponseWriter, status int, name string, binding interface{}, htmlOpt ...HTMLOptions) error {
	// If we are in development mode, recompile the templates on every HTML request.
	if r.opt.IsDevelopment {
		r.compileTemplates()
	}

	opt := r.prepareHTMLOptions(htmlOpt)
	// Assign a layout if there is one.
	if len(opt.Layout) > 0 {
		r.addLayoutFuncs(name, binding)
		name = opt.Layout
	}

	head := Head{
		ContentType: r.opt.HTMLContentType + r.compiledCharset,
		Status:      status,
	}

	h := HTML{
		Head:      head,
		Name:      name,
		Templates: r.templates,
	}

	return r.Render(w, h, binding)
} */

// JSON marshals the given interface object and writes the JSON response.
func (r *Render) JSON(w http.ResponseWriter, status int, v interface{}) error {
	head := Head{
		ContentType: ContentJSON + r.compiledCharset,
		Status:      status,
	}

	j := JSON{
		Head:          head,
		Indent:        r.opt.IndentJSON,
		Prefix:        r.opt.PrefixJSON,
		UnEscapeHTML:  r.opt.UnEscapeHTML,
		StreamingJSON: r.opt.StreamingJSON,
	}

	return r.Render(w, j, v)
}

// JSONP marshals the given interface object and writes the JSON response.
func (r *Render) JSONP(w http.ResponseWriter, status int, callback string, v interface{}) error {
	head := Head{
		ContentType: ContentJSONP + r.compiledCharset,
		Status:      status,
	}

	j := JSONP{
		Head:     head,
		Indent:   r.opt.IndentJSON,
		Callback: callback,
	}
	return r.Render(w, j, v)
}
