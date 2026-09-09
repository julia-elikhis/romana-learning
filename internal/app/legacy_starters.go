package app

// The original starter set also preserves historical question/answer snapshots.
// Migration 005 restores it to public practice alongside published course questions.
type legacyExercise struct {
	ID          string   `json:"id"`
	Prompt      string   `json:"prompt"`
	Options     []string `json:"options"`
	Answer      string   `json:"-"`
	Explanation string   `json:"-"`
}

var legacyExercises = []legacyExercise{
	{"home-1", "Choose the plural: o casă → două …", []string{"case", "casă", "casi"}, "case", "O casă, două case — a house, two houses."},
	{"home-2", "Complete: Eu … acasă.", []string{"este", "sunt", "suntem"}, "sunt", "Eu sunt = I am. Eu sunt acasă = I am at home."},
	{"home-3", "Choose the plural: un apartament → două …", []string{"apartament", "apartamente", "apartamenti"}, "apartamente", "Apartament is neuter: un apartament, două apartamente."},
	{"home-4", "Complete: Noi … o problemă.", []string{"avem", "am", "are"}, "avem", "Noi avem o problemă = We have a problem."},
	{"home-5", "Choose: two beautiful houses", []string{"două case frumoase", "două case frumos", "doi case frumoase"}, "două case frumoase", "Case is feminine plural, so use două and frumoase."},
}
