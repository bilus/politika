package main

import (
	"fmt"
	"log"
	"math"
	"math/rand"
	"sort"
	"strings"
	"sync"

	"github.com/antonmedv/expr"
	"github.com/davecgh/go-spew/spew"
	"github.com/jinzhu/copier"
	tui "github.com/marcusolsson/tui-go"
)

type World struct {
	Resources map[string]int
	Powers    map[string]int
}

func (w World) Copy() World {
	copy := World{}
	copier.Copy(&copy, &w)
	return copy
}

type Delta []float64

type Change struct {
	Resources map[string]Delta
	Powers    map[string]Delta
}

type Decision struct {
	Description string
	Choices     []Choice
}

type Choice struct {
	Description string
	Change      Change
}

type Guard struct {
	expr.Node
}

func (g Guard) Pass(world World) (bool, error) {
	out, err := expr.Run(g.Node, map[string]World{"World": world})
	if err != nil {
		return false, err
	}
	return out.(bool), nil
}

type Rule struct {
	Guard
	Weight float64
	Decision
}

func NewRule(guard string, weight float64, decision Decision) (Rule, error) {
	node, err := expr.Parse(guard, expr.Define("World", World{}))
	if err != nil {
		return Rule{}, err
	}

	return Rule{
		Guard:    Guard{node},
		Weight:   weight,
		Decision: decision,
	}, nil
}

func (r Rule) Evaluate(world World) (float64, error) {
	pass, err := r.Guard.Pass(world)
	if err != nil {
		return 0, err
	}
	if !pass {
		return 0, nil
	}
	return r.Weight, nil
}

type Scenario struct {
	Rules []Rule
}

type CandidateDecision struct {
	Weight float64
	Decision
}

type CandidateRanking []CandidateDecision

func (c CandidateRanking) Len() int {
	return len([]CandidateDecision(c))
}

func (c CandidateRanking) Swap(i, j int) {
	c[i], c[j] = c[j], c[i]
}

func (c CandidateRanking) Less(i, j int) bool {
	return c[i].Weight < c[j].Weight
}

type Rand interface {
	Float64() float64
}

type DecisionsF func(world World, maxNumDecisions int) ([]Decision, error)

func (s Scenario) Decisions(r Rand) DecisionsF {
	return func(world World, maxNumDecisions int) ([]Decision, error) {
		candidates := make([]CandidateDecision, len(s.Rules))
		for i, rule := range s.Rules {
			weight, err := rule.Evaluate(world)
			if err != nil {
				return nil, err
			}
			candidates[i] = CandidateDecision{
				Weight:   weight,
				Decision: rule.Decision,
			}
		}
		ranking := CandidateRanking(candidates)
		sort.Sort(ranking)

		decisions := make([]Decision, 0, len(candidates))
		for _, candidate := range candidates {
			if r.Float64() < candidate.Weight {
				decisions = append(decisions, candidate.Decision)
				if len(decisions) > maxNumDecisions {
					break
				}
			}
		}
		return decisions, nil
	}
}

func (w *World) Apply(choice Choice) error {
	for resource, delta := range choice.Change.Resources {
		w.Resources[resource] = updatedValue(w.Resources[resource], delta)
	}
	for power, delta := range choice.Change.Powers {
		w.Powers[power] = updatedValue(w.Powers[power], delta)
	}
	return nil
}

func updatedValue(old int, delta Delta) int {
	return int(math.Round(delta[0]*float64(old) + delta[1]))
}

func gameLoop(scenario Scenario, choiceCh <-chan Choice) (<-chan []Decision, <-chan World, error) {
	world := World{
		Resources: map[string]int{
			"Money": 4000,
		},
		Powers: map[string]int{
			"Military":    90,
			"Legislation": 10,
		},
	}

	decisionCh := make(chan []Decision)
	worldCh := make(chan World)

	go func() {
		defer close(decisionCh)
		defer close(worldCh)

		r := rand.New(rand.NewSource(0))
		for {
			worldCh <- world

			decisions, err := scenario.Decisions(r)(world, 3)
			if err != nil {
				log.Fatalf("Error getting decisions: %v", err)
			}
			if len(decisions) == 0 {
				// TODO: We need to figure out how to solve this; the game can get stuck.
				// So simple continue won't be enough.
				return
			}

			decisionCh <- decisions

			choice, ok := <-choiceCh
			if !ok {
				return
			}
			err = world.Apply(choice)
			if err != nil {
				log.Printf("Error applying choice %v to world: %v", choice.Description, err)
				return
			}
		}
	}()

	return decisionCh, worldCh, nil
}

func main() {
	rule1, err := NewRule(
		"World.Resources.Money > 1000 and World.Powers.Military >= 90",
		1.0,
		Decision{"Make putsch",
			[]Choice{
				{
					Description: "Accept",
					Change: Change{
						Resources: map[string]Delta{
							"Money":      Delta{0.5, 0},
							"Popularity": Delta{0, 0},
						},
						Powers: map[string]Delta{
							"Legislation": Delta{0, 100},
						},
					},
				}, {
					Description: "Reject",
					Change: Change{
						Powers: map[string]Delta{
							"Military": Delta{0.1, 0},
						},
					},
				},
			},
		},
	)
	rule2, err := NewRule(
		"true",
		1.0,
		Decision{"Quit",
			[]Choice{
				{
					Description: "Accept",
				},
			}})
	if err != nil {
		log.Fatalf("Error parsing expression: %v", err)
	}

	scenario := Scenario{
		Rules: []Rule{rule1, rule2},
	}

	choiceCh := make(chan Choice)
	decisionCh, worldCh, err := gameLoop(scenario, choiceCh)
	if err != nil {
		log.Fatalf("Error starting game loop: %v", err)
	}

	consoleUI(decisionCh, worldCh, choiceCh)
}

func consoleUI(decisionCh <-chan []Decision, worldCh <-chan World, choiceCh chan<- Choice) {
	debugWindow := tui.NewLabel("")
	choiceTable := tui.NewTable(0, 0)
	powerStatus := tui.NewStatusBar("")
	resourceStatus := tui.NewStatusBar("")
	root := tui.NewVBox(
		tui.NewHBox(
			tui.NewVBox(
				choiceTable,
				tui.NewSpacer(),
			),
			debugWindow),
		tui.NewSpacer(),
		tui.NewHBox(
			tui.NewVBox(
				resourceStatus,
				powerStatus,
			),
			tui.NewVBox(
				tui.NewSpacer(),
				tui.NewHBox(
					tui.NewSpacer(),
					tui.NewLabel("ESC to quit"),
				),
			),
		),
	)

	choiceTable.SetFocused(true)
	ui, err := tui.New(root)
	if err != nil {
		log.Fatal(err)
	}

	wait := sync.WaitGroup{}

	wait.Add(1)
	go func() {
		defer wait.Done()
		for world := range worldCh {
			ui.Update(func() {
				powers := make([]string, 0)
				for k, v := range world.Powers {
					powers = append(powers, fmt.Sprintf("%v: %v", k, v))
				}
				powerStatus.SetText(strings.Join(powers, " "))
				resources := make([]string, 0)
				for k, v := range world.Resources {
					resources = append(resources, fmt.Sprintf("%v: %v", k, v))
				}
				resourceStatus.SetText(strings.Join(resources, " "))
			})
		}
	}()

	wait.Add(1)
	go func() {
		defer wait.Done()

		for decisions := range decisionCh {
			ui.Update(func() {
				debugWindow.SetText(spew.Sdump(decisions))
				choiceTable.RemoveRows()

				choices := make([]Choice, 0)

				for _, decision := range decisions {
					label := tui.NewLabel(decision.Description)
					for _, choice := range decision.Choices {
						choiceBtn := tui.NewLabel(choice.Description)
						choiceTable.AppendRow(label, choiceBtn)
						choices = append(choices, choice)
					}
				}

				choiceTable.OnItemActivated(func(t *tui.Table) {
					if t.Selected() >= 0 && t.Selected() < len(choices) {
						choiceCh <- choices[t.Selected()]
					}
				})
			})
		}
	}()

	ui.SetKeybinding("Esc", func() { close(choiceCh); ui.Quit() })

	if err := ui.Run(); err != nil {
		log.Fatal(err)
	}

	wait.Wait()
}
