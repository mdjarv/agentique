// typegen generates TypeScript interfaces and Zod schemas from Go struct types.
// Run: cd backend && go run ./cmd/typegen --out ../frontend/src/lib
//
// Adding a new type:
//  1. Import the package
//  2. Call g.register(pkg.MyType{}, "MyTSName") in main()
//  3. For discriminated unions, call g.addUnion(...)
//  4. Ensure leaf types are registered before types that reference them
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/allbin/agentique/backend/internal/gitops"
	"github.com/allbin/agentique/backend/internal/msggen"
	projpkg "github.com/allbin/agentique/backend/internal/project"
	"github.com/allbin/agentique/backend/internal/session"
	"github.com/allbin/agentique/backend/internal/store"
	"github.com/allbin/agentique/backend/internal/ws"
)

var rawMessageType = reflect.TypeOf(json.RawMessage{})

type typeRef struct {
	goType reflect.Type
	tsName string
}

type unionVariant struct {
	value string
	ref   *typeRef
}

type unionDef struct {
	tsName       string
	discriminant string
	variants     []unionVariant
}

type discriminantInfo struct {
	field string
	value string
}

type generator struct {
	refs          []*typeRef
	refMap        map[reflect.Type]*typeRef
	unions        []unionDef
	discriminants map[reflect.Type]discriminantInfo
}

func newGenerator() *generator {
	return &generator{
		refMap:        make(map[reflect.Type]*typeRef),
		discriminants: make(map[reflect.Type]discriminantInfo),
	}
}

func (g *generator) register(instance any, tsName string) *typeRef {
	t := reflect.TypeOf(instance)
	ref := &typeRef{goType: t, tsName: tsName}
	g.refs = append(g.refs, ref)
	g.refMap[t] = ref
	return ref
}

func (g *generator) addUnion(tsName, discriminant string, variants []unionVariant) {
	g.unions = append(g.unions, unionDef{
		tsName:       tsName,
		discriminant: discriminant,
		variants:     variants,
	})
	for _, v := range variants {
		g.discriminants[v.ref.goType] = discriminantInfo{
			field: discriminant,
			value: v.value,
		}
	}
}

// goTypeToTS maps a Go reflect.Type to its TypeScript representation.
func (g *generator) goTypeToTS(t reflect.Type) string {
	if ref, ok := g.refMap[t]; ok {
		return ref.tsName
	}
	if t == rawMessageType {
		return "unknown"
	}
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return "string" // []byte → base64
		}
		return g.goTypeToTS(t.Elem()) + "[]"
	case reflect.Map:
		return fmt.Sprintf("Record<%s, %s>", g.goTypeToTS(t.Key()), g.goTypeToTS(t.Elem()))
	case reflect.Interface:
		return "unknown"
	case reflect.Ptr:
		return g.goTypeToTS(t.Elem())
	default:
		return "unknown"
	}
}

// goTypeToZod maps a Go reflect.Type to a Zod schema expression.
func (g *generator) goTypeToZod(t reflect.Type) string {
	if ref, ok := g.refMap[t]; ok {
		return ref.tsName + "Schema"
	}
	if t == rawMessageType {
		return "z.unknown()"
	}
	switch t.Kind() {
	case reflect.String:
		return "z.string()"
	case reflect.Bool:
		return "z.boolean()"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "z.number()"
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return "z.string()"
		}
		return fmt.Sprintf("z.array(%s)", g.goTypeToZod(t.Elem()))
	case reflect.Map:
		return fmt.Sprintf("z.record(%s, %s)", g.goTypeToZod(t.Key()), g.goTypeToZod(t.Elem()))
	case reflect.Interface:
		return "z.unknown()"
	case reflect.Ptr:
		return g.goTypeToZod(t.Elem())
	default:
		return "z.unknown()"
	}
}

func parseJSONTag(tag string) (name string, omitempty bool) {
	parts := strings.Split(tag, ",")
	name = parts[0]
	for _, p := range parts[1:] {
		if p == "omitempty" {
			omitempty = true
		}
	}
	return
}

type fieldInfo struct {
	jsonName string
	tsType   string
	zodType  string
	optional bool
}

func (g *generator) structFields(ref *typeRef) []fieldInfo {
	t := ref.goType
	disc, isVariant := g.discriminants[t]

	var fields []fieldInfo
	for i := range t.NumField() {
		sf := t.Field(i)
		tag := sf.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, omit := parseJSONTag(tag)
		tsType := g.goTypeToTS(sf.Type)
		zodType := g.goTypeToZod(sf.Type)

		// Override discriminant field with literal type.
		if isVariant && name == disc.field {
			tsType = fmt.Sprintf(`"%s"`, disc.value)
			zodType = fmt.Sprintf(`z.literal("%s")`, disc.value)
			omit = false
		}

		fields = append(fields, fieldInfo{
			jsonName: name,
			tsType:   tsType,
			zodType:  zodType,
			optional: omit,
		})
	}
	return fields
}

func (g *generator) generateTS(path string) error {
	var buf bytes.Buffer
	buf.WriteString("// Code generated by typegen. DO NOT EDIT.\n\n")

	for _, ref := range g.refs {
		fields := g.structFields(ref)
		fmt.Fprintf(&buf, "export interface %s {\n", ref.tsName)
		for _, f := range fields {
			opt := ""
			if f.optional {
				opt = "?"
			}
			fmt.Fprintf(&buf, "  %s%s: %s;\n", f.jsonName, opt, f.tsType)
		}
		buf.WriteString("}\n\n")
	}

	for _, u := range g.unions {
		parts := make([]string, len(u.variants))
		for i, v := range u.variants {
			parts[i] = v.ref.tsName
		}
		fmt.Fprintf(&buf, "export type %s =\n  | %s;\n\n", u.tsName, strings.Join(parts, "\n  | "))
	}

	return writeFile(path, buf.Bytes())
}

func (g *generator) generateZod(path string) error {
	var buf bytes.Buffer
	buf.WriteString("// Code generated by typegen. DO NOT EDIT.\n\n")
	buf.WriteString("import { z } from \"zod\";\n\n")

	for _, ref := range g.refs {
		fields := g.structFields(ref)
		fmt.Fprintf(&buf, "export const %sSchema = z.object({\n", ref.tsName)
		for _, f := range fields {
			if f.optional {
				fmt.Fprintf(&buf, "  %s: %s.optional(),\n", f.jsonName, f.zodType)
			} else {
				fmt.Fprintf(&buf, "  %s: %s,\n", f.jsonName, f.zodType)
			}
		}
		buf.WriteString("});\n\n")
	}

	for _, u := range g.unions {
		schemas := make([]string, len(u.variants))
		for i, v := range u.variants {
			schemas[i] = v.ref.tsName + "Schema"
		}
		fmt.Fprintf(&buf, "export const %sSchema = z.discriminatedUnion(\"%s\", [\n  %s,\n]);\n\n",
			u.tsName, u.discriminant, strings.Join(schemas, ",\n  "))
	}

	return writeFile(path, buf.Bytes())
}

// writeFile trims trailing blank lines and writes exactly one trailing newline.
func writeFile(path string, data []byte) error {
	content := strings.TrimRight(string(data), "\n") + "\n"
	return os.WriteFile(path, []byte(content), 0o644)
}

func main() {
	outDir := flag.String("out", "frontend/src/lib", "output directory for generated files")
	flag.Parse()

	g := newGenerator()

	// ── Leaf types (referenced by compound types — must be registered first) ──

	g.register(session.WireContentBlock{}, "WireContentBlock")
	g.register(session.QueryAttachment{}, "QueryAttachment")
	g.register(gitops.DiffStat{}, "DiffStat")
	g.register(gitops.FileStatus{}, "FileStatus")
	g.register(gitops.CommandFile{}, "CommandFile")

	// ── Wire event types (discriminated union on "type") ──

	textEvt := g.register(session.WireTextEvent{}, "WireTextEvent")
	thinkEvt := g.register(session.WireThinkingEvent{}, "WireThinkingEvent")
	toolUseEvt := g.register(session.WireToolUseEvent{}, "WireToolUseEvent")
	toolResultEvt := g.register(session.WireToolResultEvent{}, "WireToolResultEvent")
	resultEvt := g.register(session.WireResultEvent{}, "WireResultEvent")
	errorEvt := g.register(session.WireErrorEvent{}, "WireErrorEvent")
	rateLimitEvt := g.register(session.WireRateLimitEvent{}, "WireRateLimitEvent")
	streamEvt := g.register(session.WireStreamEvent{}, "WireStreamEvent")
	compactStatusEvt := g.register(session.WireCompactStatusEvent{}, "WireCompactStatusEvent")
	compactBoundaryEvt := g.register(session.WireCompactBoundaryEvent{}, "WireCompactBoundaryEvent")

	g.addUnion("WireEvent", "type", []unionVariant{
		{"text", textEvt},
		{"thinking", thinkEvt},
		{"tool_use", toolUseEvt},
		{"tool_result", toolResultEvt},
		{"result", resultEvt},
		{"error", errorEvt},
		{"rate_limit", rateLimitEvt},
		{"stream", streamEvt},
		{"compact_status", compactStatusEvt},
		{"compact_boundary", compactBoundaryEvt},
	})

	// ── Session response types ──

	g.register(session.SessionInfo{}, "SessionInfo")
	g.register(session.CreateSessionResult{}, "CreateSessionResult")
	g.register(session.ListSessionsResult{}, "ListSessionsResult")
	g.register(session.HistoryTurn{}, "HistoryTurn")
	g.register(session.HistoryResult{}, "HistoryResult")

	// ── Git operation results ──

	g.register(session.GitSnapshot{}, "GitSnapshot")
	g.register(session.MergeResult{}, "MergeResult")
	g.register(session.CreatePRResult{}, "CreatePRResult")
	g.register(session.CommitResult{}, "SessionCommitResult")
	g.register(session.RebaseResult{}, "RebaseResult")
	g.register(session.UncommittedFilesResult{}, "UncommittedFilesResult")
	g.register(session.CleanResult{}, "CleanResult")
	g.register(gitops.DiffResult{}, "DiffResult")

	// ── Message generation results ──

	g.register(msggen.CommitMessageResult{}, "CommitMessageResult")
	g.register(msggen.PRDescriptionResult{}, "PRDescriptionResult")

	// ── Project types ──

	g.register(projpkg.ProjectGitStatus{}, "ProjectGitStatus")
	g.register(projpkg.TrackedFilesResult{}, "TrackedFilesResult")
	g.register(projpkg.CommandsResult{}, "CommandsResult")
	g.register(projpkg.CommitResult{}, "ProjectCommitResult")

	// ── Store types (sqlc — uses snake_case JSON tags) ──

	g.register(store.Project{}, "Project")

	// ── WS request payloads ──

	g.register(ws.ProjectSubscribePayload{}, "ProjectSubscribePayload")
	g.register(ws.SessionCreatePayload{}, "SessionCreatePayload")
	g.register(ws.SessionQueryPayload{}, "SessionQueryPayload")
	g.register(ws.SessionListPayload{}, "SessionListPayload")
	g.register(ws.SessionStopPayload{}, "SessionStopPayload")
	g.register(ws.SessionHistoryPayload{}, "SessionHistoryPayload")
	g.register(ws.SessionDiffPayload{}, "SessionDiffPayload")
	g.register(ws.SessionInterruptPayload{}, "SessionInterruptPayload")
	g.register(ws.SessionMergePayload{}, "SessionMergePayload")
	g.register(ws.SessionCreatePRPayload{}, "SessionCreatePRPayload")
	g.register(ws.SessionDeletePayload{}, "SessionDeletePayload")
	g.register(ws.SessionDeleteBulkPayload{}, "SessionDeleteBulkPayload")
	g.register(ws.SessionDeleteBulkResultItem{}, "SessionDeleteBulkResultItem")
	g.register(ws.SessionDeleteBulkResult{}, "SessionDeleteBulkResult")
	g.register(ws.SessionSetModelPayload{}, "SessionSetModelPayload")
	g.register(ws.SessionSetPermissionPayload{}, "SessionSetPermissionPayload")
	g.register(ws.SessionResolveApprovalPayload{}, "SessionResolveApprovalPayload")
	g.register(ws.SessionSetAutoApprovePayload{}, "SessionSetAutoApprovePayload")
	g.register(ws.SessionResolveQuestionPayload{}, "SessionResolveQuestionPayload")
	g.register(ws.SessionRenamePayload{}, "SessionRenamePayload")
	g.register(ws.SessionCommitPayload{}, "SessionCommitPayload")
	g.register(ws.SessionRebasePayload{}, "SessionRebasePayload")
	g.register(ws.SessionGeneratePRDescPayload{}, "SessionGeneratePRDescPayload")
	g.register(ws.SessionGenerateCommitMsgPayload{}, "SessionGenerateCommitMsgPayload")
	g.register(ws.SessionMarkDonePayload{}, "SessionMarkDonePayload")
	g.register(ws.SessionCleanPayload{}, "SessionCleanPayload")
	g.register(ws.SessionUncommittedFilesPayload{}, "SessionUncommittedFilesPayload")
	g.register(ws.SessionUncommittedDiffPayload{}, "SessionUncommittedDiffPayload")
	g.register(ws.SessionRefreshGitPayload{}, "SessionRefreshGitPayload")
	g.register(ws.ProjectGitStatusPayload{}, "ProjectGitStatusPayload")
	g.register(ws.ProjectFetchPayload{}, "ProjectFetchPayload")
	g.register(ws.ProjectPushPayload{}, "ProjectPushPayload")
	g.register(ws.ProjectCommitPayload{}, "ProjectCommitPayload")
	g.register(ws.ProjectTrackedFilesPayload{}, "ProjectTrackedFilesPayload")
	g.register(ws.ProjectCommandsPayload{}, "ProjectCommandsPayload")
	g.register(ws.ProjectReorderPayload{}, "ProjectReorderPayload")

	// ── Generate output ──

	typesPath := filepath.Join(*outDir, "generated-types.ts")
	schemasPath := filepath.Join(*outDir, "generated-schemas.ts")

	if err := g.generateTS(typesPath); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if err := g.generateZod(schemasPath); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Generated %s (%d types, %d unions)\n", typesPath, len(g.refs), len(g.unions))
	fmt.Printf("Generated %s\n", schemasPath)
}
