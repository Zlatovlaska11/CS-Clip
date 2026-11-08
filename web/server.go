package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"zlatovlas/cs-clip/config"
	"zlatovlas/cs-clip/encoder"
	"zlatovlas/cs-clip/highlights"
	"zlatovlas/cs-clip/parser"
)

//go:embed templates
var templateFS embed.FS

// ---- public types sent to the frontend ----

type DemoEntry struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

type ParseResult struct {
	MapName    string              `json:"map_name"`
	Tickrate   float64             `json:"tickrate"`
	Duration   float64             `json:"duration_seconds"`
	KillCount  int                 `json:"kill_count"`
	Highlights []HighlightResponse `json:"highlights"`
}

type HighlightResponse struct {
	ID         int            `json:"id"`
	Type       string         `json:"type"`
	PlayerName string         `json:"player_name"`
	KillCount  int            `json:"kill_count"`
	StartTick  int            `json:"start_tick"`
	EndTick    int            `json:"end_tick"`
	Kills      []KillResponse `json:"kills"`
}

type KillResponse struct {
	KillerName string  `json:"killer_name"`
	VictimName string  `json:"victim_name"`
	Weapon     string  `json:"weapon"`
	IsHeadshot bool    `json:"is_headshot"`
	Tick       int     `json:"tick"`
	Time       float64 `json:"time"`
}

type RenderJob struct {
	ID          string    `json:"id"`
	DemoName    string    `json:"demo_name"`
	HighlightID int       `json:"highlight_id"`
	PlayerName  string    `json:"player_name"`
	KillCount   int       `json:"kill_count"`
	Status      string    `json:"status"` // pending | recording | encoding | done | error
	Message     string    `json:"message"`
	ClipFile    string    `json:"clip_file"`
	CreatedAt   time.Time `json:"created_at"`
}

type ClipEntry struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

// ---- server ----

type demoCache struct {
	info       *parser.DemoInfo
	highlights []highlights.Highlight
}

type Server struct {
	cfg        *config.Config
	mu         sync.RWMutex
	demoCache  map[string]*demoCache
	renderJobs map[string]*RenderJob
}

func NewServer(cfg *config.Config) *Server {
	return &Server{
		cfg:        cfg,
		demoCache:  make(map[string]*demoCache),
		renderJobs: make(map[string]*RenderJob),
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// Static HTML
	mux.HandleFunc("GET /{$}", s.handleIndex)

	// API
	mux.HandleFunc("GET /api/demos", s.handleListDemos)
	mux.HandleFunc("POST /api/demos/upload", s.handleUploadDemo)
	mux.HandleFunc("POST /api/parse", s.handleParse)
	mux.HandleFunc("POST /api/render", s.handleRender)
	mux.HandleFunc("GET /api/jobs", s.handleListJobs)
	mux.HandleFunc("GET /api/jobs/{id}", s.handleJob)
	mux.HandleFunc("GET /api/clips", s.handleListClips)

	// Serve rendered clips
	mux.Handle("GET /clips/", http.StripPrefix("/clips/",
		http.FileServer(http.Dir(s.cfg.ClipsFolder))))

	return mux
}

// ---- handlers ----

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	data, err := templateFS.ReadFile("templates/index.html")
	if err != nil {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func (s *Server) handleUploadDemo(w http.ResponseWriter, r *http.Request) {
	maxBytes := int64(s.cfg.MaxUploadSizeGB) * 1024 * 1024 * 1024
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "failed to parse upload: "+err.Error())
		return
	}

	file, header, err := r.FormFile("demo")
	if err != nil {
		writeError(w, http.StatusBadRequest, "demo file required")
		return
	}
	defer file.Close()

	if filepath.Ext(header.Filename) != ".dem" {
		writeError(w, http.StatusBadRequest, "only .dem files are accepted")
		return
	}

	name := filepath.Base(header.Filename)
	dest := filepath.Join(s.cfg.DemosFolder, name)

	out, err := os.Create(dest)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create file: "+err.Error())
		return
	}
	defer out.Close()

	size, err := io.Copy(out, file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save demo: "+err.Error())
		return
	}

	writeJSON(w, DemoEntry{Name: name, Size: size, ModTime: time.Now()})
}

func (s *Server) handleListDemos(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(s.cfg.DemosFolder)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, []DemoEntry{})
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var demos []DemoEntry
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".dem" {
			continue
		}
		info, _ := e.Info()
		demos = append(demos, DemoEntry{
			Name:    e.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}
	sort.Slice(demos, func(i, j int) bool {
		return demos[i].ModTime.After(demos[j].ModTime)
	})
	writeJSON(w, demos)
}

func (s *Server) handleParse(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Demo string `json:"demo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Demo == "" {
		writeError(w, http.StatusBadRequest, "demo name required")
		return
	}

	s.mu.RLock()
	cached, ok := s.demoCache[req.Demo]
	s.mu.RUnlock()

	if !ok {
		path := filepath.Join(s.cfg.DemosFolder, filepath.Base(req.Demo))
		info, err := parser.ParseDemo(path)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		hl := highlights.Detect(info)
		cached = &demoCache{info: info, highlights: hl}

		s.mu.Lock()
		s.demoCache[req.Demo] = cached
		s.mu.Unlock()
	}

	result := toParseResult(cached)
	writeJSON(w, result)
}

func (s *Server) handleRender(w http.ResponseWriter, r *http.Request) {
	maxBytes := int64(s.cfg.MaxUploadSizeGB) * 1024 * 1024 * 1024
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "failed to parse upload: "+err.Error())
		return
	}

	demoName := r.FormValue("demo")
	highlightIDStr := r.FormValue("highlight_id")
	if demoName == "" || highlightIDStr == "" {
		writeError(w, http.StatusBadRequest, "demo and highlight_id required")
		return
	}

	var highlightID int
	if _, err := fmt.Sscan(highlightIDStr, &highlightID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid highlight_id")
		return
	}

	s.mu.RLock()
	cached, ok := s.demoCache[demoName]
	s.mu.RUnlock()
	if !ok {
		writeError(w, http.StatusBadRequest, "demo not parsed yet")
		return
	}

	var target *highlights.Highlight
	for i := range cached.highlights {
		if cached.highlights[i].ID == highlightID {
			target = &cached.highlights[i]
			break
		}
	}
	if target == nil {
		writeError(w, http.StatusNotFound, "highlight not found")
		return
	}

	videoFile, videoHeader, err := r.FormFile("video")
	if err != nil {
		writeError(w, http.StatusBadRequest, "video file required")
		return
	}
	defer videoFile.Close()

	// Save uploaded video to a temp file.
	ext := filepath.Ext(videoHeader.Filename)
	if ext == "" {
		ext = ".mp4"
	}
	tmpVideo, err := os.CreateTemp("", "cs2clip_upload_*"+ext)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create temp file")
		return
	}
	defer os.Remove(tmpVideo.Name())

	if _, err := io.Copy(tmpVideo, videoFile); err != nil {
		tmpVideo.Close()
		writeError(w, http.StatusInternalServerError, "failed to save upload")
		return
	}
	tmpVideo.Close()

	// Convert ticks to seconds using the demo's tickrate.
	tickrate := cached.info.Tickrate
	if tickrate == 0 {
		tickrate = float64(s.cfg.Tickrate)
	}
	startSec := float64(target.StartTick) / tickrate
	durationSec := float64(target.EndTick-target.StartTick) / tickrate

	clipName := fmt.Sprintf("%s_%dk_%d.mp4",
		sanitize(target.PlayerName), target.KillCount, target.StartTick)
	clipPath := filepath.Join(s.cfg.ClipsFolder, clipName)

	if err := encoder.CutClip(tmpVideo.Name(), startSec, durationSec, clipPath); err != nil {
		writeError(w, http.StatusInternalServerError, "ffmpeg: "+err.Error())
		return
	}

	writeJSON(w, map[string]string{"clip_file": clipName})
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	jobs := make([]*RenderJob, 0, len(s.renderJobs))
	for _, j := range s.renderJobs {
		jobs = append(jobs, j)
	}
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].CreatedAt.After(jobs[j].CreatedAt)
	})
	writeJSON(w, jobs)
}

func (s *Server) handleJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.RLock()
	job, ok := s.renderJobs[id]
	s.mu.RUnlock()
	if !ok {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	writeJSON(w, job)
}

func (s *Server) handleListClips(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(s.cfg.ClipsFolder)
	if err != nil {
		writeJSON(w, []ClipEntry{})
		return
	}
	var clips []ClipEntry
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".mp4" {
			continue
		}
		info, _ := e.Info()
		clips = append(clips, ClipEntry{
			Name:    e.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}
	sort.Slice(clips, func(i, j int) bool {
		return clips[i].ModTime.After(clips[j].ModTime)
	})
	writeJSON(w, clips)
}

// ---- helpers ----

func toParseResult(c *demoCache) ParseResult {
	r := ParseResult{
		MapName:   c.info.MapName,
		Tickrate:  c.info.Tickrate,
		Duration:  c.info.Duration.Seconds(),
		KillCount: len(c.info.Kills),
	}
	for _, h := range c.highlights {
		hr := HighlightResponse{
			ID:         h.ID,
			Type:       h.Type,
			PlayerName: h.PlayerName,
			KillCount:  h.KillCount,
			StartTick:  h.StartTick,
			EndTick:    h.EndTick,
		}
		for _, k := range h.Kills {
			hr.Kills = append(hr.Kills, KillResponse{
				KillerName: k.KillerName,
				VictimName: k.VictimName,
				Weapon:     k.Weapon,
				IsHeadshot: k.IsHeadshot,
				Tick:       k.Tick,
				Time:       k.TimeSeconds,
			})
		}
		r.Highlights = append(r.Highlights, hr)
	}
	return r
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func sanitize(s string) string {
	out := make([]byte, 0, len(s))
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_' {
			out = append(out, c)
		} else {
			out = append(out, '_')
		}
	}
	return string(out)
}
