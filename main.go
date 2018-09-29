package main

import (
	"fmt"
	"log"
	"math"
	"math/rand"
	"sort"

	"github.com/antonmedv/expr"
	"github.com/davecgh/go-spew/spew"
)

type World struct {
	Resources map[string]int
	Powers    map[string]int
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

func (w *World) Apply(choice Choice) (World, error) {
	for resource, delta := range choice.Change.Resources {
		w.Resources[resource] = updatedValue(w.Resources[resource], delta)
	}
	for power, delta := range choice.Change.Powers {
		w.Powers[power] = updatedValue(w.Powers[power], delta)
	}
	return *w, nil
}

func updatedValue(old int, delta Delta) int {
	return int(math.Round(delta[0]*float64(old) + delta[1]))
}

func main() {
	rule, err := NewRule(
		"World.Resources.Money > 1000 and World.Powers.Military >= 90",
		1.0,
		Decision{"Make putsch",
			[]Choice{
				{
					Description: "Yes",
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
					Change: Change{
						Powers: map[string]Delta{
							"Military": Delta{0.1, 0},
						},
					},
				},
			},
		},
	)
	if err != nil {
		log.Fatalf("Error parsing expression: %v", err)
	}

	scenario := Scenario{
		Rules: []Rule{rule},
	}

	world := World{
		Resources: map[string]int{
			"Money": 4000,
		},
		Powers: map[string]int{
			"Military":    90,
			"Legislation": 10,
		},
	}

	r := rand.New(rand.NewSource(0))
	decisions, err := scenario.Decisions(r)(world, 1)
	if err != nil {
		log.Fatalf("Error getting decisions: %v", err)
	}

	for _, decision := range decisions {
		fmt.Println(decision.Description)
	}

	spew.Dump(world)
	if len(decisions) > 0 {
		world.Apply(decisions[0].Choices[0])
	}

	spew.Dump(world)

}
