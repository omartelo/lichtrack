// Command demo is a YouTrack that answers the reads youtrack-lich makes with an
// invented team, for the screenshots screenshots.sh takes. Only the reads are
// answered faithfully: a write is not kept, and most fail to decode.
//
//	go run ./demo [addr]
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type obj = map[string]any

var now = time.Now()

func ms(t time.Time) int64      { return t.UnixMilli() }
func ago(d time.Duration) int64 { return ms(now.Add(-d)) }

const day = 24 * time.Hour

func user(id, login, name string) obj {
	return obj{"id": id, "login": login, "fullName": name, "email": login + "@atlas.example", "avatarUrl": ""}
}

var (
	ana    = user("1-1", "ana", "Ana Souza")
	bruno  = user("1-2", "bruno", "Bruno Lima")
	carla  = user("1-3", "carla", "Carla Mendes")
	diego  = user("1-4", "diego", "Diego Rocha")
	elisa  = user("1-5", "elisa", "Elisa Prado")
	felipe = user("1-6", "felipe", "Felipe Nunes")
	people = []obj{ana, bruno, carla, diego, elisa, felipe}
)

var project = obj{"id": "0-1", "name": "Atlas", "shortName": "ATL", "archived": false}

var states = []struct {
	name     string
	resolved bool
}{{"A fazer", false}, {"Em andamento", false}, {"Em revisão", false}, {"Concluído", true}}

type card struct {
	n        string
	summary  string
	state    string
	priority string
	kind     string
	who      obj
	minutes  int
	tags     []string
	desc     string
	updated  time.Duration
}

var cards = []card{
	{"ATL-142", "Rastreamento do entregador em tempo real no mapa", "Em andamento", "Alta", "Feature", ana, 960, []string{"mobile"}, descTracking, 2 * time.Hour},
	{"ATL-151", "Push quando o pedido sai para entrega", "Em andamento", "Normal", "Feature", bruno, 300, []string{"mobile"}, "", 5 * time.Hour},
	{"ATL-138", "Checkout trava ao trocar o cartão salvo", "Em revisão", "Crítica", "Bug", carla, 240, []string{"pagamentos"}, "", 3 * time.Hour},
	{"ATL-147", "Cupom aplicado duas vezes no carrinho", "A fazer", "Alta", "Bug", diego, 120, []string{"pagamentos"}, "", day},
	{"ATL-153", "Tela de avaliação do pedido", "A fazer", "Normal", "Feature", elisa, 480, nil, "", 2 * day},
	{"ATL-155", "Modo escuro nas telas de conta", "A fazer", "Baixa", "Tarefa", felipe, 360, []string{"design"}, "", 3 * day},
	{"ATL-149", "Endereços favoritos sincronizados entre aparelhos", "Em andamento", "Normal", "Feature", diego, 600, nil, "", 6 * time.Hour},
	{"ATL-133", "Migrar o login para o novo provedor de identidade", "Em revisão", "Alta", "Tarefa", bruno, 720, []string{"backend"}, "", 9 * time.Hour},
	{"ATL-129", "Tempo estimado de entrega por bairro", "Concluído", "Normal", "Feature", ana, 480, nil, "", 2 * day},
	{"ATL-131", "Erro 500 ao listar pedidos antigos", "Concluído", "Alta", "Bug", carla, 180, []string{"backend"}, "", 3 * day},
	{"ATL-157", "Filtro por restaurante aberto agora", "A fazer", "Normal", "Feature", ana, 240, nil, "", 4 * time.Hour},
	{"ATL-135", "Relatório semanal de pedidos cancelados", "Concluído", "Baixa", "Tarefa", elisa, 120, nil, "", 4 * day},
	{"ATL-158", "Cache das imagens dos cardápios", "Em andamento", "Normal", "Tarefa", ana, 300, []string{"mobile"}, "", 7 * time.Hour},
	{"ATL-136", "Fuso horário errado no histórico de pedidos", "Em revisão", "Alta", "Bug", ana, 150, []string{"backend"}, "", 8 * time.Hour},
	{"ATL-159", "Rótulos de acessibilidade nos botões do mapa", "A fazer", "Normal", "Tarefa", ana, 180, []string{"mobile"}, "", 30 * time.Hour},
	{"ATL-160", "Chat com o entregador", "A fazer", "Normal", "Feature", ana, 720, nil, "", 2 * day},
}

const descTracking = `O cliente acompanha o entregador no mapa desde a saída do restaurante até a porta.

## Critérios de aceite

- A posição atualiza a cada 5 segundos enquanto o pedido está **a caminho**.
- O tempo estimado é recalculado quando o entregador desvia da rota.
- Sem sinal de GPS por mais de 1 minuto, o mapa mostra a última posição conhecida e um aviso.

## Fora do escopo

Chat com o entregador fica para o ATL-160.`

func enumValue(name string) obj { return obj{"id": name, "name": name} }

func issueJSON(c card) obj {
	resolved := any(nil)
	for _, s := range states {
		if s.name == c.state && s.resolved {
			resolved = ago(c.updated)
		}
	}
	tags := []obj{}
	for _, t := range c.tags {
		tags = append(tags, obj{"id": t, "name": t})
	}
	desc := c.desc
	if desc == "" {
		desc = "Detalhes no documento do squad."
	}
	return obj{
		"id": "2-" + strings.TrimPrefix(c.n, "ATL-"), "idReadable": c.n, "summary": c.summary, "description": desc,
		"created": ago(c.updated + 5*day), "updated": ago(c.updated), "resolved": resolved,
		"project": project, "reporter": carla, "tags": tags,
		"customFields": []obj{
			{"id": "f1", "name": "Prioridade", "$type": "SingleEnumIssueCustomField", "value": enumValue(c.priority)},
			{"id": "f2", "name": "Tipo", "$type": "SingleEnumIssueCustomField", "value": enumValue(c.kind)},
			{"id": "f3", "name": "Estado", "$type": "StateIssueCustomField", "value": obj{"id": c.state, "name": c.state, "isResolved": c.state == "Concluído"}},
			{"id": "f4", "name": "Responsável", "$type": "SingleUserIssueCustomField", "value": c.who},
			{"id": "f5", "name": "Estimativa", "$type": "PeriodIssueCustomField", "value": obj{"id": "p", "minutes": c.minutes, "presentation": period(c.minutes)}},
		},
	}
}

func period(m int) string {
	h, r := m/60, m%60
	switch {
	case h > 0 && r > 0:
		return itoa(h) + "h " + itoa(r) + "m"
	case h > 0:
		return itoa(h) + "h"
	}
	return itoa(r) + "m"
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }

func find(id string) (card, bool) {
	for _, c := range cards {
		if c.n == id || "2-"+strings.TrimPrefix(c.n, "ATL-") == id {
			return c, true
		}
	}
	return card{}, false
}

var stateField = obj{"id": "cf-3", "name": "Estado", "fieldType": obj{"id": "state[1]", "valueType": "state", "isMultiValue": false}}

func sprint(id, name string, start, finish time.Time) obj {
	return obj{"id": id, "name": name, "goal": "", "start": ms(start), "finish": ms(finish), "archived": false, "isDefault": false, "unresolvedIssuesCount": 8}
}

var (
	monday  = time.Date(now.Year(), now.Month(), now.Day()-(int(now.Weekday())+6)%7, 0, 0, 0, 0, time.Local)
	current = sprint("s-24", "Sprint 24", monday, monday.AddDate(0, 0, 13))
	sprints = []obj{
		sprint("s-22", "Sprint 22", monday.AddDate(0, 0, -28), monday.AddDate(0, 0, -15)),
		sprint("s-23", "Sprint 23", monday.AddDate(0, 0, -14), monday.AddDate(0, 0, -1)),
		current,
	}
)

func agile(id, name string, favorite bool) obj {
	cols := []obj{}
	for i, s := range states {
		cols = append(cols, obj{"id": "c" + itoa(i), "presentation": s.name, "isResolved": s.resolved, "ordinal": i,
			"fieldValues": []obj{{"id": s.name, "name": s.name, "isResolved": s.resolved}}})
	}
	return obj{"id": id, "name": name, "favorite": favorite, "projects": []obj{project}, "currentSprint": current,
		"columnSettings": obj{"field": stateField, "columns": cols}}
}

var boards = []obj{agile("b-1", "Atlas · App", true), agile("b-2", "Atlas · Suporte", false)}

func bundle(names ...string) obj {
	vals := []obj{}
	for _, n := range names {
		vals = append(vals, obj{"id": n, "name": n, "description": "", "archived": false, "isResolved": n == "Concluído", "released": false})
	}
	return obj{"id": "bd", "values": vals}
}

func schemaField(id, name, typeID, valueType string, b obj) obj {
	return obj{"id": id, "$type": "ProjectCustomField", "canBeEmpty": true, "emptyFieldText": "Sem valor",
		"field": obj{"id": "cf-" + id, "name": name, "fieldType": obj{"id": typeID, "valueType": valueType, "isMultiValue": false}}, "bundle": b}
}

var schema = []obj{
	schemaField("1", "Prioridade", "enum[1]", "enum", bundle("Crítica", "Alta", "Normal", "Baixa")),
	schemaField("2", "Tipo", "enum[1]", "enum", bundle("Bug", "Feature", "Tarefa")),
	schemaField("3", "Estado", "state[1]", "state", bundle("A fazer", "Em andamento", "Em revisão", "Concluído")),
	schemaField("4", "Responsável", "user[1]", "user", obj{"id": "bu", "aggregatedUsers": people}),
	schemaField("5", "Estimativa", "period", "period", nil),
}

var workTypes = []obj{{"id": "w1", "name": "Desenvolvimento"}, {"id": "w2", "name": "Revisão"}, {"id": "w3", "name": "Reunião"}}

func workItem(id string, c card, at time.Time, minutes int, text string, typ obj) obj {
	return obj{"id": id, "text": text, "date": ms(at), "created": ms(at), "updated": nil,
		"duration": obj{"minutes": minutes, "presentation": period(minutes)}, "type": typ, "author": ana,
		"issue": obj{"idReadable": c.n, "summary": c.summary}}
}

func week(start time.Time) []obj {
	plan := []struct {
		n       string
		day     int
		minutes int
		typ     obj
	}{
		{"ATL-142", 0, 240, workTypes[0]}, {"ATL-142", 1, 300, workTypes[0]}, {"ATL-142", 2, 180, workTypes[0]}, {"ATL-142", 3, 240, workTypes[0]},
		{"ATL-129", 0, 120, workTypes[0]}, {"ATL-129", 1, 60, workTypes[1]},
		{"ATL-138", 2, 90, workTypes[1]}, {"ATL-138", 3, 60, workTypes[1]},
		{"ATL-157", 3, 120, workTypes[0]}, {"ATL-157", 4, 210, workTypes[0]},
		{"ATL-133", 1, 60, workTypes[1]}, {"ATL-133", 4, 90, workTypes[1]},
		{"ATL-153", 0, 60, workTypes[2]}, {"ATL-153", 2, 60, workTypes[2]}, {"ATL-153", 4, 60, workTypes[2]},
	}
	items := []obj{}
	for i, p := range plan {
		at := start.AddDate(0, 0, p.day).Add(12 * time.Hour)
		if at.After(now) {
			continue
		}
		c, _ := find(p.n)
		items = append(items, workItem("wi"+itoa(i), c, at, p.minutes, "", p.typ))
	}
	return items
}

var saved = []obj{
	{"id": "q1", "name": "Meus cards abertos", "query": "for: me #Unresolved"},
	{"id": "q2", "name": "Bugs da sprint", "query": "project: ATL Tipo: Bug Board Atlas · App: {Sprint 24}"},
	{"id": "q3", "name": "Em revisão", "query": "project: ATL Estado: {Em revisão}"},
	{"id": "q4", "name": "Sem responsável", "query": "project: ATL Responsável: Unassigned"},
	{"id": "q5", "name": "Atualizados hoje", "query": "project: ATL updated: Today"},
}

func comments(c card) []obj {
	if c.n != "ATL-142" {
		return []obj{}
	}
	return []obj{
		{"id": "k1", "text": "O SDK de mapas já expõe a posição em stream; dá pra evitar o polling.", "created": ago(26 * time.Hour), "updated": nil, "author": bruno},
		{"id": "k2", "text": "Boa. Fiz o protótipo com o stream e a bateria ficou ok no aparelho de teste.", "created": ago(20 * time.Hour), "updated": nil, "author": ana},
		{"id": "k3", "text": "Lembrem do aviso de GPS perdido, o suporte recebe muitas reclamações disso.", "created": ago(3 * time.Hour), "updated": nil, "author": elisa},
	}
}

func links(c card) []obj {
	if c.n != "ATL-142" {
		return []obj{}
	}
	rel := obj{"id": "lt1", "name": "Relates", "sourceToTarget": "relacionado a", "targetToSource": "relacionado a", "directed": false}
	dep := obj{"id": "lt2", "name": "Depend", "sourceToTarget": "depende de", "targetToSource": "é necessário para", "directed": true}
	ref := func(n string) obj {
		o, _ := find(n)
		return obj{"id": "2-" + strings.TrimPrefix(n, "ATL-"), "idReadable": n, "summary": o.summary, "resolved": nil}
	}
	return []obj{
		{"id": "lt2s", "direction": "OUTWARD", "linkType": dep, "issues": []obj{ref("ATL-129")}},
		{"id": "lt1", "direction": "BOTH", "linkType": rel, "issues": []obj{ref("ATL-151")}},
	}
}

func attachments(c card) []obj {
	if c.n != "ATL-142" {
		return []obj{}
	}
	return []obj{{"id": "a1", "name": "fluxo-rastreamento.png", "size": 184320, "mimeType": "image/png", "extension": "png", "created": ago(2 * day), "url": "/files/a1", "author": carla}}
}

func activities(c card) []obj {
	field := func(name string) obj { return obj{"id": name, "name": name, "presentation": name} }
	return []obj{
		{"id": "h3", "timestamp": ago(2 * time.Hour), "targetMember": "", "author": ana, "category": obj{"id": "CustomFieldCategory"}, "field": field("Estado"), "added": []obj{{"name": "Em andamento"}}, "removed": []obj{{"name": "A fazer"}}},
		{"id": "h2", "timestamp": ago(30 * time.Hour), "targetMember": "", "author": carla, "category": obj{"id": "CustomFieldCategory"}, "field": field("Responsável"), "added": []obj{{"fullName": "Ana Souza", "login": "ana"}}, "removed": []obj{}},
		{"id": "h1", "timestamp": ago(c.updated + 5*day), "targetMember": "", "author": carla, "category": obj{"id": "IssueCreatedCategory"}, "field": nil, "added": nil, "removed": nil},
	}
}

func search(query string) []obj {
	q := strings.ToLower(query)
	out := []obj{}
	for _, c := range cards {
		switch {
		case strings.Contains(q, "for: me") && c.who["login"] != "ana":
			continue
		case strings.Contains(q, "#unresolved") && c.state == "Concluído":
			continue
		case strings.Contains(q, "tipo: bug") && c.kind != "Bug":
			continue
		case strings.Contains(q, "em revisão") && c.state != "Em revisão":
			continue
		}
		out = append(out, issueJSON(c))
	}
	return out
}

func main() {
	addr := ":47920"
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}
	log.Printf("demo YouTrack on %s", addr)
	log.Fatal(http.ListenAndServe(addr, handler()))
}

func handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL)
		parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api"), "/"), "/")
		out, ok := route(r, parts)
		if !ok {
			http.Error(w, `{"error":"not_found","error_description":"the demo has no `+r.URL.Path+`"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	})
}

func route(r *http.Request, p []string) (any, bool) {
	q := r.URL.Query()
	switch {
	case len(p) == 1 && p[0] == "agiles":
		return boards, true
	case len(p) == 2 && p[0] == "agiles":
		for _, b := range boards {
			if b["id"] == p[1] {
				return b, true
			}
		}
	case len(p) == 3 && p[0] == "agiles" && p[2] == "sprints":
		return sprints, true
	case len(p) == 4 && p[0] == "agiles" && p[2] == "sprints":
		s := obj{}
		for k, v := range current {
			s[k] = v
		}
		issues := []obj{}
		for _, c := range cards {
			issues = append(issues, issueJSON(c))
		}
		if p[1] == "b-2" {
			issues = issues[:4]
		}
		s["issues"] = issues
		return s, true
	case len(p) == 1 && p[0] == "issues":
		if r.Method == http.MethodPost {
			return issueJSON(cards[0]), true
		}
		return search(q.Get("query")), true
	case len(p) >= 2 && p[0] == "issues":
		c, found := find(p[1])
		if !found {
			return nil, false
		}
		if len(p) == 2 {
			return issueJSON(c), true
		}
		switch p[2] {
		case "comments":
			return comments(c), true
		case "links":
			return links(c), true
		case "attachments":
			return attachments(c), true
		case "activities":
			return activities(c), true
		case "timeTracking":
			items := []obj{}
			for _, it := range week(monday.AddDate(0, 0, -7)) {
				if it["issue"].(obj)["idReadable"] == c.n {
					items = append(items, it)
				}
			}
			return items, true
		case "tags":
			return obj{}, true
		}
	case len(p) == 1 && p[0] == "commands":
		return obj{}, true
	case len(p) == 1 && p[0] == "savedQueries":
		return saved, true
	case len(p) == 1 && p[0] == "tags":
		return []obj{{"id": "mobile", "name": "mobile"}, {"id": "pagamentos", "name": "pagamentos"}, {"id": "backend", "name": "backend"}, {"id": "design", "name": "design"}}, true
	case len(p) == 1 && p[0] == "issueLinkTypes":
		return []obj{}, true
	case len(p) == 2 && p[0] == "users" && p[1] == "me":
		return ana, true
	case len(p) == 1 && p[0] == "users":
		return people, true
	case len(p) == 2 && p[0] == "admin" && p[1] == "projects":
		return []obj{project}, true
	case len(p) == 4 && p[0] == "admin" && p[3] == "customFields":
		return schema, true
	case len(p) >= 4 && p[0] == "admin" && p[3] == "timeTrackingSettings":
		return workTypes, true
	case len(p) == 1 && p[0] == "workItems":
		start, err := time.ParseInLocation("2006-01-02", q.Get("startDate"), time.Local)
		if err != nil {
			start = monday
		}
		return week(start), true
	}
	return nil, false
}
