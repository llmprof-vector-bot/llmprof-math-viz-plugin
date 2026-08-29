package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/extism/go-pdk"
)

// ---------------------------------------------------------------------------
// Embedded KaTeX assets
// ---------------------------------------------------------------------------

//go:embed assets/katex.min.js
var katexJS string

//go:embed assets/katex.min.css
var katexCSS string

// ---------------------------------------------------------------------------
// Host capability helpers
// ---------------------------------------------------------------------------

//go:wasmimport extism:host/user call_host_capability
func callHostCapabilityRaw(ptr uint64) uint64

func requestCapability(capabilityName string, inputObj any) string {
	payload := map[string]any{
		"capability": capabilityName,
		"input":      inputObj,
	}
	reqBytes, _ := json.Marshal(payload)
	memIn := pdk.AllocateBytes(reqBytes)
	memOutOffset := callHostCapabilityRaw(memIn.Offset())
	memOut := pdk.FindMemory(memOutOffset)
	return string(memOut.ReadBytes())
}

func logHost(level, message string) {
	_ = requestCapability("log", map[string]any{
		"level":   level,
		"message": message,
	})
}

func logDebug(message string) { logHost("debug", message) }
func logInfo(message string)  { logHost("info", message) }
func logError(message string) { logHost("error", message) }

// ---------------------------------------------------------------------------
// Storage helpers
// ---------------------------------------------------------------------------

func storageWrite(filename string, data []byte) error {
	if err := os.MkdirAll("/storage", 0755); err != nil {
		return fmt.Errorf("failed to create /storage: %w", err)
	}
	path := "/storage/" + filename
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	return nil
}

func storageRead(filename string) ([]byte, error) {
	path := "/storage/" + filename
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}
	return data, nil
}

func storageExists(filename string) bool {
	path := "/storage/" + filename
	_, err := os.Stat(path)
	return err == nil
}

// ---------------------------------------------------------------------------
// Data structures
// ---------------------------------------------------------------------------

type Step struct {
	Tex                  string `json:"tex"`
	Note                 string `json:"note"`
	DetailedExplanation  string `json:"detailed_explanation,omitempty"`
}

type VizData struct {
	Title string `json:"title"`
	Steps []Step `json:"steps"`
}

// ---------------------------------------------------------------------------
// Tool entry point
// ---------------------------------------------------------------------------

//go:wasmexport renderFormulaVisualization
func renderFormulaVisualization() int32 {
	logInfo("renderFormulaVisualization: entry point called")

	inputBytes := pdk.Input()
	var inputData map[string]any
	if err := json.Unmarshal(inputBytes, &inputData); err != nil {
		logError(fmt.Sprintf("renderFormulaVisualization: failed to parse input JSON: %v", err))
		pdk.SetError(errors.New("invalid JSON input from host"))
		return 1
	}

	mode, _ := inputData["mode"].(string)
	switch mode {
	case "define":
		return handleDefine(inputData)
	case "execute":
		return handleExecute(inputData)
	case "applet":
		return handleApplet(inputData)
	default:
		pdk.SetError(fmt.Errorf("unknown mode: %q", mode))
		return 1
	}
}

// ---------------------------------------------------------------------------
// Define mode — return tool schema
// ---------------------------------------------------------------------------

func handleDefine(inputData map[string]any) int32 {
	logInfo("renderFormulaVisualization: define mode")

	definition := map[string]any{
		"mode": "define",
		"name": "renderFormulaVisualization",
		"description": "Render a step-by-step mathematical derivation, proof, or formula manipulation as an interactive animated visualization with LaTeX rendering. Use this tool whenever a mathematical calculation, equation derivation, proof, or formula transformation should be shown step by step - even if the user doesn't explicitly ask for steps. Any time math involves multiple steps of algebraic manipulation, this tool should be used to provide a visual walkthrough.",
		"parameters": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title": map[string]any{
					"type":        "string",
					"description": "A concise title for the mathematical derivation",
				},
				"steps": map[string]any{
					"type":     "array",
					"minItems": 1,
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"tex": map[string]any{
								"type":        "string",
								"description": "Valid LaTeX code for this step's equation",
							},
							"note": map[string]any{
								"type":        "string",
								"description": "Short label for this step (max ~80 chars). Keep it concise - use detailed_explanation for longer explanations.",
							},
							"detailed_explanation": map[string]any{
								"type":        "string",
								"description": "Optional: detailed explanation of this step. If provided, an info button in the visualization will show this text.",
							},
						},
						"required": []string{"tex", "note"},
					},
					"description": "Array of derivation steps, each with LaTeX and a note.",
				},
			},
			"required": []string{"title", "steps"},
		},
	}

	outputJSON, err := json.Marshal(definition)
	if err != nil {
		pdk.SetError(fmt.Errorf("failed to marshal definition: %w", err))
		return 1
	}
	pdk.OutputString(string(outputJSON))
	return 0
}

// ---------------------------------------------------------------------------
// Execute mode — validate, store data, return LLM prompt + applet_id
// ---------------------------------------------------------------------------

func handleExecute(inputData map[string]any) int32 {
	logInfo("renderFormulaVisualization: execute mode")

	args, _ := inputData["arguments"].(map[string]any)
	if args == nil {
		pdk.SetError(errors.New("missing arguments"))
		return 1
	}

	// Validate title
	title, _ := args["title"].(string)
	if strings.TrimSpace(title) == "" {
		pdk.SetError(errors.New("title must be non-empty"))
		return 1
	}

	// Validate steps
	stepsRaw, _ := args["steps"].([]any)
	if len(stepsRaw) < 1 {
		pdk.SetError(errors.New("steps must have at least 1 entry"))
		return 1
	}

	var steps []Step
	for i, sRaw := range stepsRaw {
		s, ok := sRaw.(map[string]any)
		if !ok {
			pdk.SetError(fmt.Errorf("step %d is not an object", i))
			return 1
		}
		tex, _ := s["tex"].(string)
		note, _ := s["note"].(string)
		detailedExplanation, _ := s["detailed_explanation"].(string)
		if strings.TrimSpace(tex) == "" {
			pdk.SetError(fmt.Errorf("step %d: tex must be non-empty", i))
			return 1
		}
		if strings.TrimSpace(note) == "" {
			pdk.SetError(fmt.Errorf("step %d: note must be non-empty", i))
			return 1
		}
		steps = append(steps, Step{
			Tex:                 tex,
			Note:                note,
			DetailedExplanation: detailedExplanation,
		})
	}

	// Build storage data
	vizData := VizData{
		Title: title,
		Steps: steps,
	}
	storageBytes, _ := json.Marshal(vizData)

	// Store the JSON
	timestamp := time.Now().Unix()
	storageFile := fmt.Sprintf("mathviz-%d.json", timestamp)
	if err := storageWrite(storageFile, storageBytes); err != nil {
		logError(fmt.Sprintf("failed to write storage: %v", err))
		pdk.SetError(fmt.Errorf("storage write failed: %w", err))
		return 1
	}

	// Also write KaTeX assets to /storage/ if they don't exist yet
	if !storageExists("katex.min.js") {
		if err := storageWrite("katex.min.js", []byte(katexJS)); err != nil {
			logError(fmt.Sprintf("failed to write katex.min.js: %v", err))
		}
	}
	if !storageExists("katex.min.css") {
		if err := storageWrite("katex.min.css", []byte(katexCSS)); err != nil {
			logError(fmt.Sprintf("failed to write katex.min.css: %v", err))
		}
	}

	appletID := fmt.Sprintf("mathviz-%d", timestamp)

	// Build content as an LLM prompt (not the actual math content)
	content := fmt.Sprintf(
		"You have generated a mathematical visualization with %d steps. The visualization is shown as an interactive applet to the user.\n\n"+
			"IMPORTANT:\n"+
			"- Do NOT repeat the mathematical steps or LaTeX in your text response.\n"+
			"- Refer the user to the visualization applet.\n"+
			"- If the user asked a specific question, answer it briefly.\n"+
			"- Keep your response short (1-3 sentences). For example: \"Hier kannst du Schritt für Schritt nachvollziehen, wie die Gleichung aufgelöst wird:\" or point out a specific interesting step.",
		len(steps),
	)

	result := map[string]any{
		"mode": "execute",
		"result": map[string]any{
			"content": content,
			"ui": map[string]any{
				"applet_id": appletID,
			},
		},
	}

	outputJSON, err := json.Marshal(result)
	if err != nil {
		pdk.SetError(fmt.Errorf("failed to marshal result: %w", err))
		return 1
	}
	pdk.OutputString(string(outputJSON))
	return 0
}

// ---------------------------------------------------------------------------
// Applet mode — generate self-contained HTML
// ---------------------------------------------------------------------------

func handleApplet(inputData map[string]any) int32 {
	logInfo("renderFormulaVisualization: applet mode")

	appletID, _ := inputData["applet_id"].(string)
	if appletID == "" {
		pdk.SetError(errors.New("missing applet_id"))
		return 1
	}

	// Read step data from storage
	storageFile := fmt.Sprintf("%s.json", appletID)
	data, err := storageRead(storageFile)
	if err != nil {
		logError(fmt.Sprintf("failed to read storage %s: %v", storageFile, err))
		pdk.SetError(fmt.Errorf("storage read failed: %w", err))
		return 1
	}

	var vizData VizData
	if err := json.Unmarshal(data, &vizData); err != nil {
		pdk.SetError(fmt.Errorf("failed to parse storage data: %w", err))
		return 1
	}

	// Read KaTeX assets from storage
	katexJSContent, err := storageRead("katex.min.js")
	if err != nil {
		logError(fmt.Sprintf("failed to read katex.min.js from storage: %v", err))
		// Fall back to embedded assets
		katexJSContent = []byte(katexJS)
	}
	katexCSSContent, err := storageRead("katex.min.css")
	if err != nil {
		logError(fmt.Sprintf("failed to read katex.min.css from storage: %v", err))
		// Fall back to embedded assets
		katexCSSContent = []byte(katexCSS)
	}

	html := buildAppletHTML(vizData, katexJSContent, katexCSSContent)

	result := map[string]any{
		"mode": "applet",
		"html": html,
	}

	outputJSON, err := json.Marshal(result)
	if err != nil {
		pdk.SetError(fmt.Errorf("failed to marshal applet result: %w", err))
		return 1
	}
	pdk.OutputString(string(outputJSON))
	return 0
}

// ---------------------------------------------------------------------------
// HTML generation helpers
// ---------------------------------------------------------------------------

func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

func jsonEscape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	return s
}

func buildAppletHTML(data VizData, katexJSBytes []byte, katexCSSBytes []byte) string {
	// Build step data JSON for embedding
	stepDataJSON, _ := json.Marshal(data.Steps)
	stepDataStr := string(stepDataJSON)

	var parts []string

	// HTML head
	parts = append(parts, "<!DOCTYPE html>\n<html lang=\"de\">\n<head>\n")
	parts = append(parts, "<meta charset=\"UTF-8\">\n")
	parts = append(parts, "<meta name=\"viewport\" content=\"width=device-width, initial-scale=1.0\">\n")
	parts = append(parts, fmt.Sprintf("<title>%s</title>\n", htmlEscape(data.Title)))
	parts = append(parts, "<style>\n")

	// Inline KaTeX CSS
	parts = append(parts, string(katexCSSBytes))
	parts = append(parts, "\n")

	// Applet CSS
	parts = append(parts, appletCSS())

	parts = append(parts, "</style>\n</head>\n<body>\n")
	// Container
	parts = append(parts, "<div class=\"mv-container\">\n")

	// Header
	parts = append(parts, fmt.Sprintf("<div class=\"mv-header\">%s</div>\n", htmlEscape(data.Title)))

	// Formula area with side navigation buttons
	parts = append(parts, "<div class=\"mv-formula-area\">\n")

	// Previous button
	parts = append(parts, "<button class=\"mv-nav-btn mv-prev\" id=\"mv-prev\" aria-label=\"Previous step\">&lsaquo;</button>\n")

	// Formula display
	parts = append(parts, "<div class=\"mv-formula-display\">\n")
	parts = append(parts, "  <div class=\"mv-prev-step\" id=\"mv-prev-step\"></div>\n")
	parts = append(parts, "  <div class=\"mv-active-step\" id=\"mv-active-step\"></div>\n")
	parts = append(parts, "  <div class=\"mv-note\" id=\"mv-note\"></div>\n")
	// Info button row for detailed explanation
	parts = append(parts, "  <div class=\"mv-info-row\">\n")
	parts = append(parts, "    <button class=\"mv-info-btn\" id=\"mv-info-btn\" style=\"display:none;\" aria-label=\"Detailed explanation\">ⓘ</button>\n")
	parts = append(parts, "    <div class=\"mv-info-panel\" id=\"mv-info-panel\" style=\"display:none;\"></div>\n")
	parts = append(parts, "  </div>\n")
	parts = append(parts, "</div>\n")

	// Next button
	parts = append(parts, "<button class=\"mv-nav-btn mv-next\" id=\"mv-next\" aria-label=\"Next step\">&rsaquo;</button>\n")

	parts = append(parts, "</div>\n") // end formula-area

	// Step badges
	parts = append(parts, "<div class=\"mv-badges\" id=\"mv-badges\">\n")
	for i := range data.Steps {
		isLast := i == len(data.Steps)-1
		classes := "mv-badge"
		if isLast {
			classes += " mv-badge-last"
		}
		parts = append(parts, fmt.Sprintf("<button class=\"%s\" data-step=\"%d\">%d</button>\n", classes, i, i+1))
	}
	parts = append(parts, "</div>\n")

	parts = append(parts, "</div>\n") // end container

	// KaTeX JS inline
	parts = append(parts, "<script>\n")
	parts = append(parts, string(katexJSBytes))
	parts = append(parts, "\n")

	// Step data embedded
	parts = append(parts, "var mvSteps = ")
	parts = append(parts, stepDataStr)
	parts = append(parts, ";\n")
	parts = append(parts, "var mvTitle = \"")
	parts = append(parts, jsonEscape(data.Title))
	parts = append(parts, "\";\n")

	// Applet JavaScript
	parts = append(parts, appletJS(len(data.Steps)))

	parts = append(parts, "</script>\n")
	parts = append(parts, "</body>\n</html>")

	return strings.Join(parts, "")
}

// ---------------------------------------------------------------------------
// Applet CSS
// ---------------------------------------------------------------------------

func appletCSS() string {
	var s []string

	s = append(s, `
/* Reset & base */
* { margin: 0; padding: 0; box-sizing: border-box; }
body {
  height: 320px;
  overflow: hidden;
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
  background: #ffffff;
  color: #1a1a1a;
  -webkit-font-smoothing: antialiased;
}

.mv-container {
  display: flex;
  flex-direction: column;
  height: 320px;
  padding: 12px 16px;
}

/* Header */
.mv-header {
  text-align: center;
  font-size: 14px;
  font-weight: 600;
  color: #2d7d2d;
  padding-bottom: 6px;
  border-bottom: 1px solid #e0e0e0;
  margin-bottom: 8px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* Formula area */
.mv-formula-area {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  position: relative;
  min-height: 0;
}

.mv-formula-display {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  text-align: center;
  padding: 0 8px;
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
}

/* Previous step (faded, above) */
.mv-prev-step {
  opacity: 0;
  transform: translateY(10px);
  transition: opacity 300ms ease, transform 300ms ease;
  font-size: 20px;
  color: #999;
  margin-bottom: 8px;
  min-height: 28px;
}

.mv-prev-step.mv-visible {
  opacity: 0.5;
  transform: translateY(0);
}

/* Active step */
.mv-active-step {
  font-size: 28px;
  transition: opacity 300ms ease, transform 300ms ease;
  min-height: 40px;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 8px 0;
  overflow-x: auto;
  -webkit-overflow-scrolling: touch;
}

.mv-active-step.mv-fade-out {
  opacity: 0;
  transform: translateY(-10px);
}

/* Result card for final step */
.mv-active-step.mv-result {
  background: #f0f7f0;
  border: 2px solid #2d7d2d;
  border-radius: 8px;
  padding: 12px 20px;
  margin: 4px 0;
}

/* Note text */
.mv-note {
  font-size: 13px;
  color: #666;
  margin-top: 8px;
  transition: opacity 300ms ease;
  min-height: 18px;
  text-align: center;
  max-width: 90%;
}

.mv-note.mv-fade {
  opacity: 0;
}

/* Info button row */
.mv-info-row { margin-top: 4px; text-align: center; }
.mv-info-btn {
  border: 1px solid #ccc; background: #f8f8f8; color: #666;
  border-radius: 50%; width: 22px; height: 22px; font-size: 13px;
  cursor: pointer; display: inline-flex; align-items: center; justify-content: center;
  transition: all 200ms ease;
}
.mv-info-btn:hover { border-color: #2d7d2d; color: #2d7d2d; }
.mv-info-panel {
  margin-top: 6px; padding: 8px 12px; background: #f8f8f8; border-radius: 6px;
  font-size: 12px; color: #555; text-align: left; max-width: 90%;
  display: none; border: 1px solid #e0e0e0;
}
.mv-info-panel.mv-show { display: block; }

/* Navigation buttons */
.mv-nav-btn {
  flex-shrink: 0;
  width: 36px;
  height: 36px;
  border-radius: 50%;
  border: 2px solid #ddd;
  background: #fff;
  color: #2d7d2d;
  font-size: 20px;
  font-weight: bold;
  cursor: pointer;
  transition: all 200ms ease;
  display: flex;
  align-items: center;
  justify-content: center;
}

.mv-nav-btn:hover {
  border-color: #2d7d2d;
  background: #f0f7f0;
}

.mv-nav-btn:disabled {
  opacity: 0.3;
  cursor: default;
}

/* Step badges */
.mv-badges {
  display: flex;
  justify-content: center;
  align-items: center;
  gap: 6px;
  padding-top: 8px;
  flex-shrink: 0;
}

.mv-badge {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  border: 2px solid #ddd;
  background: #fff;
  color: #666;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  transition: all 200ms ease;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0;
}

.mv-badge:hover {
  border-color: #2d7d2d;
  color: #2d7d2d;
}

.mv-badge.mv-active {
  background: #2d7d2d;
  color: #fff;
  border-color: #2d7d2d;
}

.mv-badge-last.mv-active {
  background: #2d7d2d;
  color: #fff;
  border-color: #2d7d2d;
}

.mv-badge-last {
  border-color: #2d7d2d;
  color: #2d7d2d;
}

/* KaTeX size adjustments */
.mv-active-step .katex {
  font-size: 1.2em;
}

.mv-prev-step .katex {
  font-size: 1.0em;
}

/* Responsive */
@media (max-width: 400px) {
  .mv-active-step { font-size: 22px; }
  .mv-header { font-size: 12px; }
  .mv-badge { width: 24px; height: 24px; font-size: 10px; }
}
`)

	return strings.Join(s, "")
}

// ---------------------------------------------------------------------------
// Applet JavaScript
// ---------------------------------------------------------------------------

func appletJS(numSteps int) string {
	var s []string

	s = append(s, `
(function() {
  var currentStep = 0;
  var totalSteps = `)
	s = append(s, fmt.Sprintf("%d", numSteps))
	s = append(s, `;
  var prevStepEl = document.getElementById('mv-prev-step');
  var activeStepEl = document.getElementById('mv-active-step');
  var noteEl = document.getElementById('mv-note');
  var prevBtn = document.getElementById('mv-prev');
  var nextBtn = document.getElementById('mv-next');
  var badgesEl = document.getElementById('mv-badges');
  var infoBtn = document.getElementById('mv-info-btn');
  var infoPanel = document.getElementById('mv-info-panel');

  function renderStep(tex, element) {
    try {
      katex.render(tex, element, {
        throwOnError: false,
        displayMode: true
      });
    } catch(e) {
      element.textContent = tex;
    }
  }

  function renderNoteInline(text, element) {
    element.innerHTML = '';
    // Split text by $...$ and \(...\) delimiters, render only the math parts with KaTeX
    var parts = [];
    var regex = /(\$[^$]+\$|\\\([^)]+\\\))/g;
    var lastIndex = 0;
    var match;
    while ((match = regex.exec(text)) !== null) {
      if (match.index > lastIndex) {
        parts.push({type: 'text', value: text.substring(lastIndex, match.index)});
      }
      parts.push({type: 'math', value: match[0]});
      lastIndex = match.index + match[0].length;
    }
    if (lastIndex < text.length) {
      parts.push({type: 'text', value: text.substring(lastIndex)});
    }
    if (parts.length === 0) {
      element.textContent = text;
      return;
    }
    for (var i = 0; i < parts.length; i++) {
      if (parts[i].type === 'text') {
        var span = document.createElement('span');
        span.textContent = parts[i].value;
        element.appendChild(span);
      } else {
        var mathSpan = document.createElement('span');
        element.appendChild(mathSpan);
        var mathText = parts[i].value;
        if (mathText.charAt(0) === '$') {
          mathText = mathText.substring(1, mathText.length - 1);
        } else {
          mathText = mathText.substring(2, mathText.length - 2);
        }
        try {
          katex.render(mathText, mathSpan, {throwOnError: false, displayMode: false});
        } catch(e) {
          mathSpan.textContent = parts[i].value;
        }
      }
    }
  }

  function updateDisplay() {
    // Fade out current
    activeStepEl.classList.add('mv-fade-out');
    noteEl.classList.add('mv-fade');
    prevStepEl.classList.remove('mv-visible');

    setTimeout(function() {
      // Render active step
      var step = mvSteps[currentStep];
      renderStep(step.tex, activeStepEl);

      // Render previous step if exists
      if (currentStep > 0) {
        renderStep(mvSteps[currentStep - 1].tex, prevStepEl);
        prevStepEl.classList.add('mv-visible');
      }

      // Update note (render with KaTeX inline mode)
      renderNoteInline(step.note, noteEl);

      // Handle detailed explanation info button
      var hasDetailed = step.detailed_explanation && step.detailed_explanation.trim() !== '';
      if (hasDetailed) {
        infoBtn.style.display = 'inline-flex';
        // Reset panel state
        infoPanel.classList.remove('mv-show');
        infoPanel.style.display = 'none';
        // Render detailed explanation with hybrid LaTeX/text renderer
        renderNoteInline(step.detailed_explanation, infoPanel);
        // Set up click handler (remove old, add new)
        var newBtn = infoBtn.cloneNode(true);
        infoBtn.parentNode.replaceChild(newBtn, infoBtn);
        infoBtn = newBtn;
        infoBtn.addEventListener('click', function() {
          if (infoPanel.classList.contains('mv-show')) {
            infoPanel.classList.remove('mv-show');
            infoPanel.style.display = 'none';
          } else {
            infoPanel.classList.add('mv-show');
            infoPanel.style.display = 'block';
          }
        });
      } else {
        infoBtn.style.display = 'none';
        infoPanel.classList.remove('mv-show');
        infoPanel.style.display = 'none';
      }

      // Check if last step (result)
      if (currentStep === totalSteps - 1) {
        activeStepEl.classList.add('mv-result');
      } else {
        activeStepEl.classList.remove('mv-result');
      }

      // Fade in
      activeStepEl.classList.remove('mv-fade-out');
      noteEl.classList.remove('mv-fade');

      // Update buttons
      prevBtn.disabled = (currentStep === 0);
      nextBtn.disabled = (currentStep === totalSteps - 1);

      // Update badges
      var badges = badgesEl.querySelectorAll('.mv-badge');
      badges.forEach(function(b, i) {
        if (i === currentStep) {
          b.classList.add('mv-active');
        } else {
          b.classList.remove('mv-active');
        }
      });
    }, 150);
  }

  function goToStep(idx) {
    if (idx < 0 || idx >= totalSteps) return;
    currentStep = idx;
    updateDisplay();
  }

  function nextStep() {
    if (currentStep < totalSteps - 1) {
      currentStep++;
      updateDisplay();
    }
  }

  function prevStep() {
    if (currentStep > 0) {
      currentStep--;
      updateDisplay();
    }
  }

  // Event listeners
  prevBtn.addEventListener('click', prevStep);
  nextBtn.addEventListener('click', nextStep);

  // Badge clicks
  badgesEl.addEventListener('click', function(e) {
    if (e.target.classList.contains('mv-badge')) {
      var step = parseInt(e.target.getAttribute('data-step'));
      goToStep(step);
    }
  });

  // Keyboard navigation
  document.addEventListener('keydown', function(e) {
    if (e.key === 'ArrowLeft') {
      e.preventDefault();
      prevStep();
    } else if (e.key === 'ArrowRight') {
      e.preventDefault();
      nextStep();
    }
  });

  // Initial render
  updateDisplay();
})();
`)

	return strings.Join(s, "")
}

// ---------------------------------------------------------------------------
// Lifecycle hooks
// ---------------------------------------------------------------------------

//go:wasmexport on_install
func onInstall() int32 {
	logInfo("math-viz-plugin: on_install called")
	os.MkdirAll("/storage", 0755)
	os.WriteFile("/storage/katex.min.js", []byte(katexJS), 0644)
	os.WriteFile("/storage/katex.min.css", []byte(katexCSS), 0644)
	return 0
}

//go:wasmexport on_uninstall
func onUninstall() int32 {
	logInfo("math-viz-plugin: on_uninstall called")
	return 0
}

func main() {}