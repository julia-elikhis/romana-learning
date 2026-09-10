package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/julia-elikhis/romana-learning/internal/materials"
)

type Material struct {
	ID             string    `json:"id"`
	RootID         string    `json:"rootId"`
	Revision       int       `json:"revision"`
	Title          string    `json:"title"`
	Filename       string    `json:"filename"`
	Kind           string    `json:"kind"`
	Reviewed       bool      `json:"reviewed"`
	Text           string    `json:"text,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	DraftCount     int       `json:"draftCount"`
	PublishedCount int       `json:"publishedCount"`
	Generated      bool      `json:"generated"`
}

const materialFields = `m.id,m.root_id,m.revision,m.title,m.filename,m.kind,m.reviewed,m.created_at,
 (SELECT count(*) FROM exercises e WHERE e.material_id=m.id AND e.status='draft'),
 (SELECT count(*) FROM exercises e WHERE e.material_id=m.id AND e.status='published'),
 EXISTS(SELECT 1 FROM exercises e WHERE e.material_id=m.id)`

func scanMaterial(row interface{ Scan(...any) error }, withText bool) (Material, error) {
	var m Material
	dest := []any{&m.ID, &m.RootID, &m.Revision, &m.Title, &m.Filename, &m.Kind, &m.Reviewed, &m.CreatedAt, &m.DraftCount, &m.PublishedCount, &m.Generated}
	if withText {
		dest = append(dest, &m.Text)
	}
	err := row.Scan(dest...)
	return m, err
}
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func contextFor(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 25*time.Second)
}
func fail(w http.ResponseWriter, status int, message string) {
	respond(w, status, map[string]string{"error": message})
}
func decode(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(&struct{}{}) != io.EOF {
		fail(w, 400, "Invalid request body")
		return false
	}
	return true
}

func (s Server) library(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextFor(r)
	defer cancel()
	rows, err := s.DB.QueryContext(ctx, `SELECT `+materialFields+` FROM materials m ORDER BY m.created_at DESC,m.id`)
	if err != nil {
		fail(w, 503, "Could not load course materials")
		return
	}
	defer rows.Close()
	list := []Material{}
	for rows.Next() {
		m, err := scanMaterial(rows, false)
		if err != nil {
			fail(w, 503, "Could not read course materials")
			return
		}
		list = append(list, m)
	}
	if rows.Err() != nil {
		fail(w, 503, "Could not read course materials")
		return
	}
	respond(w, 200, map[string]any{"enabled": s.Files != nil, "apiEnabled": s.Generator.Ready(), "generator": materials.GeneratorVersion, "materials": list})
}
func (s Server) upload(w http.ResponseWriter, r *http.Request) {
	if s.Files == nil {
		fail(w, 503, "Course file storage is disabled on this server")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, materials.MaxUpload+(1<<20))
	if err := r.ParseMultipartForm(materials.MaxUpload); err != nil {
		fail(w, 400, "Upload a file up to 20 MB")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	title := strings.TrimSpace(r.FormValue("title"))
	kind := r.FormValue("kind")
	replaces := r.FormValue("replacesId")
	if title == "" || len(title) > 200 {
		fail(w, 400, "Provide a title under 200 characters")
		return
	}
	if kind != "notes" && kind != "teacher_corrections" && kind != "homework" && kind != "student_submission" {
		fail(w, 400, "Choose the material's role")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		fail(w, 400, "Choose a document")
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, materials.MaxUpload+1))
	if err != nil || len(raw) > materials.MaxUpload {
		fail(w, 400, "File must be smaller than 20 MB")
		return
	}
	filename := filepath.Base(header.Filename)
	if len(filename) > 255 {
		fail(w, 400, "Filename is too long")
		return
	}
	ctx, cancel := contextFor(r)
	defer cancel()
	text, err := materials.Extract(ctx, filename, raw)
	if err != nil {
		fail(w, 422, err.Error())
		return
	}
	digest := sha256.Sum256(raw)
	hash := hex.EncodeToString(digest[:])
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		fail(w, 503, "Could not save the document")
		return
	}
	defer tx.Rollback()
	// Serialize imports so duplicate retries and replacement revision numbers are stable.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(726662032)`); err != nil {
		fail(w, 503, "Could not save the document")
		return
	}
	id := newID()
	root := id
	revision := 1
	if replaces != "" {
		var previousKind string
		err = tx.QueryRowContext(ctx, `SELECT root_id,kind FROM materials WHERE id=$1`, replaces).Scan(&root, &previousKind)
		if err != nil {
			fail(w, 404, "The previous document was not found")
			return
		}
		if previousKind != kind {
			fail(w, 400, "A new revision must keep the same material role")
			return
		}
		if err = tx.QueryRowContext(ctx, `SELECT max(revision)+1 FROM materials WHERE root_id=$1`, root).Scan(&revision); err != nil {
			fail(w, 503, "Could not create revision")
			return
		}
	}
	var duplicate string
	if replaces == "" {
		err = tx.QueryRowContext(ctx, `SELECT id FROM materials WHERE content_hash=$1 AND kind=$2 AND title=$3 ORDER BY created_at LIMIT 1`, hash, kind, title).Scan(&duplicate)
	} else {
		err = tx.QueryRowContext(ctx, `SELECT id FROM materials WHERE root_id=$1 AND content_hash=$2`, root, hash).Scan(&duplicate)
	}
	if err == nil {
		respond(w, 200, map[string]any{"id": duplicate, "duplicate": true})
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		fail(w, 503, "Could not check existing documents")
		return
	}
	key := id + ".original"
	if err = s.Files.Put(ctx, key, raw); err != nil {
		fail(w, 503, "Could not store the original file. Check the server's storage configuration")
		return
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO materials(id,root_id,revision,title,filename,kind,content_hash,object_key,extracted_text,owner_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, id, root, revision, title, filename, kind, hash, key, text, currentUser(r).ID)
	if err != nil {
		fail(w, 503, "Could not save document metadata")
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, 503, "Could not confirm the upload. Retry the same file to check")
		return
	}
	respond(w, 201, map[string]any{"id": id, "duplicate": false})
}

const exerciseFields = `id,kind,prompt,options,answers,explanation,source_quote,source_line,status`

func scanDraft(row interface{ Scan(...any) error }) (materials.Draft, error) {
	var d materials.Draft
	var options, answers []byte
	err := row.Scan(&d.ID, &d.Kind, &d.Prompt, &options, &answers, &d.Explanation, &d.SourceQuote, &d.SourceLine, &d.Status)
	if err == nil {
		err = json.Unmarshal(options, &d.Options)
	}
	if err == nil {
		err = json.Unmarshal(answers, &d.Answers)
	}
	return d, err
}
func (s Server) material(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextFor(r)
	defer cancel()
	id := r.PathValue("id")
	m, err := scanMaterial(s.DB.QueryRowContext(ctx, `SELECT `+materialFields+`,m.extracted_text FROM materials m WHERE m.id=$1`, id), true)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "Material not found")
		return
	}
	if err != nil {
		fail(w, 503, "Could not read the document")
		return
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+exerciseFields+` FROM exercises WHERE material_id=$1 AND status<>'deleted' ORDER BY source_line,id`, id)
	if err != nil {
		fail(w, 503, "Could not read drafts")
		return
	}
	defer rows.Close()
	drafts := []materials.Draft{}
	for rows.Next() {
		d, err := scanDraft(rows)
		if err != nil {
			fail(w, 503, "Could not read drafts")
			return
		}
		drafts = append(drafts, d)
	}
	if rows.Err() != nil {
		fail(w, 503, "Could not read drafts")
		return
	}
	respond(w, 200, map[string]any{"material": m, "exercises": drafts, "generator": materials.GeneratorVersion})
}
func (s Server) reviewSource(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text     string `json:"text"`
		Reviewed bool   `json:"reviewed"`
	}
	if !decode(w, r, &body, materials.MaxText+4096) {
		return
	}
	if len(body.Text) > materials.MaxText || strings.TrimSpace(body.Text) == "" || strings.ContainsRune(body.Text, 0) {
		fail(w, 400, "Provide nonempty text under 200 KB")
		return
	}
	ctx, cancel := contextFor(r)
	defer cancel()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		fail(w, 503, "Could not save teaching text")
		return
	}
	defer tx.Rollback()
	var kind string
	err = tx.QueryRowContext(ctx, `SELECT kind FROM materials WHERE id=$1 FOR UPDATE`, r.PathValue("id")).Scan(&kind)
	if err != nil {
		if err != sql.ErrNoRows {
			fail(w, 503, "Could not load the document")
			return
		}
		fail(w, 404, "Material not found")
		return
	}
	if kind == "homework" || kind == "student_submission" {
		fail(w, 400, "Homework and learner submissions are reference material. Upload teaching notes or teacher corrections to generate exercises")
		return
	}
	var locked bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM exercises WHERE material_id=$1)`, r.PathValue("id")).Scan(&locked); err != nil {
		fail(w, 503, "Could not save teaching text")
		return
	}
	if locked {
		fail(w, 409, "The source is locked after generation. Upload a new revision to change it")
		return
	}
	if _, err = tx.ExecContext(ctx, `UPDATE materials SET extracted_text=$2,reviewed=true WHERE id=$1`, r.PathValue("id"), body.Text); err != nil {
		fail(w, 503, "Could not save teaching text")
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, 503, "Could not confirm teaching text")
		return
	}
	respond(w, 200, map[string]bool{"reviewed": true})
}
func (s Server) generate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Count     int    `json:"count"`
		Mode      string `json:"mode"`
		SendToAPI bool   `json:"sendToApi"`
	}
	if !decode(w, r, &body, 1024) {
		return
	}
	if body.Count < 1 || body.Count > 20 {
		fail(w, 400, "Choose between 1 and 20 exercises")
		return
	}
	if body.Mode != "" && body.Mode != "local" && body.Mode != "api" {
		fail(w, 400, "Choose local or api generation")
		return
	}
	if body.Mode == "api" && !s.Generator.Ready() {
		fail(w, 400, "Configure the generation API before generating with AI")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), materials.GenerationTimeout)
	defer cancel()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		fail(w, 503, "Could not generate drafts")
		return
	}
	defer tx.Rollback()
	id := r.PathValue("id")
	var source, kind string
	err = tx.QueryRowContext(ctx, `SELECT extracted_text,kind FROM materials WHERE id=$1 FOR UPDATE`, id).Scan(&source, &kind)
	if err != nil {
		if err != sql.ErrNoRows {
			fail(w, 503, "Could not load the document")
			return
		}
		fail(w, 404, "Material not found")
		return
	}
	if kind != "notes" && kind != "teacher_corrections" {
		fail(w, 409, "Generate from teaching notes or teacher corrections")
		return
	}
	var existing int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM exercises WHERE material_id=$1`, id).Scan(&existing); err != nil {
		fail(w, 503, "Could not read drafts")
		return
	}
	if existing > 0 {
		respond(w, 200, map[string]any{"count": existing, "existing": true})
		return
	}
	var drafts []materials.Draft
	skipped := []materials.GenerationIssue{}
	version := materials.GeneratorVersion
	if body.Mode == "api" {
		var generated materials.GenerationResult
		generated, err = s.Generator.Generate(ctx, source, id, body.Count)
		drafts, skipped = generated.Exercises, generated.Skipped
		version = materials.APIGeneratorVersion
	} else {
		drafts, err = materials.Generate(source, id, body.Count)
	}
	if err != nil {
		fail(w, 422, err.Error())
		return
	}
	for _, d := range drafts {
		if err = materials.Validate(d); err != nil {
			fail(w, 422, "Could not produce valid drafts; edit the teaching examples and try again")
			return
		}
		options, _ := json.Marshal(d.Options)
		answers, _ := json.Marshal(d.Answers)
		_, err = tx.ExecContext(ctx, `INSERT INTO exercises(id,material_id,kind,prompt,options,answers,explanation,source_quote,source_line,status,generator_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'draft',$10)`, d.ID, id, d.Kind, d.Prompt, string(options), string(answers), d.Explanation, d.SourceQuote, d.SourceLine, version)
		if err != nil {
			fail(w, 503, "Could not save exercise drafts")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		fail(w, 503, "Could not confirm generation. Retry to load any saved drafts")
		return
	}
	respond(w, 201, map[string]any{"count": len(drafts), "existing": false, "skipped": skipped, "languageReviewed": body.Mode == "api"})
}
func (s Server) editExercise(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind        string   `json:"kind"`
		Prompt      string   `json:"prompt"`
		Options     []string `json:"options"`
		Answers     []string `json:"answers"`
		Explanation string   `json:"explanation"`
	}
	if !decode(w, r, &body, 8192) {
		return
	}
	ctx, cancel := contextFor(r)
	defer cancel()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		fail(w, 503, "Could not save question")
		return
	}
	defer tx.Rollback()
	d, err := scanDraft(tx.QueryRowContext(ctx, `SELECT `+exerciseFields+` FROM exercises WHERE id=$1 AND status<>'deleted' FOR UPDATE`, r.PathValue("id")))
	if err != nil {
		if err != sql.ErrNoRows {
			fail(w, 503, "Could not load the exercise")
			return
		}
		fail(w, 404, "Exercise not found")
		return
	}
	d.Kind = body.Kind
	d.Prompt = body.Prompt
	d.Options = body.Options
	d.Answers = body.Answers
	d.Explanation = body.Explanation
	if d.Options == nil {
		d.Options = []string{}
	}
	if err = materials.Validate(d); err != nil {
		fail(w, 400, err.Error())
		return
	}
	options, _ := json.Marshal(d.Options)
	answers, _ := json.Marshal(d.Answers)
	_, err = tx.ExecContext(ctx, `UPDATE exercises SET kind=$2,prompt=$3,options=$4,answers=$5,explanation=$6 WHERE id=$1`, d.ID, d.Kind, d.Prompt, string(options), string(answers), d.Explanation)
	if err != nil {
		fail(w, 503, "Could not save question")
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, 503, "Could not confirm save")
		return
	}
	respond(w, 200, d)
}
func (s Server) publishExercise(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Status   string `json:"status"`
		Reviewed bool   `json:"reviewed"`
	}
	if !decode(w, r, &body, 1024) {
		return
	}
	if body.Status != "published" && body.Status != "rejected" {
		fail(w, 400, "Choose published or rejected")
		return
	}
	ctx, cancel := contextFor(r)
	defer cancel()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		fail(w, 503, "Could not update exercise")
		return
	}
	defer tx.Rollback()
	d, err := scanDraft(tx.QueryRowContext(ctx, `SELECT `+exerciseFields+` FROM exercises WHERE id=$1 AND material_id IS NOT NULL FOR UPDATE`, r.PathValue("id")))
	if err != nil {
		if err != sql.ErrNoRows {
			fail(w, 503, "Could not load the draft")
			return
		}
		fail(w, 404, "Draft not found")
		return
	}
	if d.Status == body.Status {
		respond(w, 200, map[string]string{"status": d.Status})
		return
	}
	if d.Status != "draft" {
		fail(w, 409, "This exercise has already been reviewed")
		return
	}
	if body.Status == "published" {
		if err = materials.Validate(d); err != nil {
			fail(w, 400, err.Error())
			return
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE exercises SET status=$2 WHERE id=$1`, d.ID, body.Status); err != nil {
		fail(w, 503, "Could not update exercise")
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, 503, "Could not confirm exercise status")
		return
	}
	respond(w, 200, map[string]string{"status": body.Status})
}
func (s Server) download(w http.ResponseWriter, r *http.Request) {
	if s.Files == nil {
		fail(w, 503, "File storage is disabled")
		return
	}
	ctx, cancel := contextFor(r)
	defer cancel()
	var key, name string
	if err := s.DB.QueryRowContext(ctx, `SELECT object_key,filename FROM materials WHERE id=$1`, r.PathValue("id")).Scan(&key, &name); err != nil {
		fail(w, 404, "Original document not found")
		return
	}
	reader, err := s.Files.Open(ctx, key)
	if err != nil {
		fail(w, 503, "Could not open the original file")
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	_, _ = io.Copy(w, reader)
}
