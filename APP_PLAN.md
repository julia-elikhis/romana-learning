# Romanian practice app — initial product plan

Research and material review: 9 September 2026. Draft for discussion; no implementation chosen yet.

## Product purpose

A personal companion to private Romanian lessons that turns course material and teacher feedback into enjoyable daily practice, progressing toward B1 communication and the user's specific exam.

Design promise: open the app and immediately know what to do. Make starting easy while keeping recall, listening, and language production meaningfully challenging.

## What the available materials establish

The actual folder is `/Users/julia/Documents/romainan`. Inventory: 53 DOCX files, nine PDFs, and one M4A recording. There are A1 lessons numbered 1–15 (14–15 combined), homework, submitted homework, teacher versions, recap exercises, and A1/A2 textbook material. Sampled lesson text covers introductions, numbers, time, present-tense conjugation, noun gender and plural, agreement, prepositions, and home vocabulary. Explanations mix Romanian, Hebrew, and English.

This review sampled DOCX text; it did not fully audit every correction, PDF, or the audio. File labels establish curriculum coverage, not the learner's current proficiency. Available material does not establish complete B1 coverage. The user confirmed December 2026 and supplied RoExam's B1 specification. The exact day remains open. See [EXAM_PLAN.md](EXAM_PLAN.md) for verified format, scoring implications, and the revised roadmap.

Teacher corrections are promising personalization inputs, but a file named “teacher” is not automatically a clean answer key. Preserve source context and distinguish prompts, learner answers, corrections, and incidental edits. The notes include typos and simplified rules; generated exercises need review before entering the learning queue.

## Research and implications

| Finding | Evidence and limits | Product decision |
|---|---|---|
| Spacing improves retention | A second-language meta-analysis covered 48 experiments and 3,411 participants; longer intervals benefited delayed tests. Equal and expanding schedules were statistically equivalent. | Revisit skills across days; adapt intervals to recall, rather than repeatedly drilling a finished lesson. |
| Retrieval strengthens memory | A foreign-vocabulary experiment found repeated testing improved delayed recall after initial success; repeated studying did not in that experiment. | Ask for an answer before revealing it. Use typed and spoken production alongside recognition. |
| Corrective feedback can support learning | Classroom research supports feedback, with effects depending on its form and context. It does not validate automated grading. | Give short explanations and another attempt; prioritize a few useful corrections in free speech. |
| Meaningful input matters | Extensive-reading research supports learning from substantial accessible reading; a tiny story alone is not equivalent to an extensive-reading program. | Include understandable texts and dialogues, gradually increasing length, plus optional longer reading. |
| Gamification can help, with variable effects | A meta-analysis found positive learning effects; motivational and behavioral effects were less robust in stronger-study subsets. | Treat mechanics as hypotheses to test with this learner, rather than guaranteed motivators. |
| Small commitments can improve return rates | Duolingo reports an experiment separating streak completion from the daily goal increased day-14 retention by 3.3%. This is company-reported engagement evidence. | Offer a tiny minimum session and a separate substantive practice goal. |
| Dopamine participates in reward learning | Neuroscience describes reward prediction-error signals, not a validated formula for an app's colors, points, or animations. | Design for anticipation, enjoyable feedback, novelty, and visible competence; do not claim to measure or optimize dopamine. |

Sources:

- [Kim & Webb: spaced practice meta-analysis](https://doi.org/10.1111/lang.12479)
- [Karpicke & Roediger: retrieval experiment](https://doi.org/10.1126/science.1152408)
- [Lyster, Saito & Sato: corrective feedback research](https://www.cambridge.org/core/journals/language-teaching/article/abs/oral-corrective-feedback-in-second-language-classrooms/B33FC71C12A1317DCA0CE704B7B0BC00)
- [Nakanishi: extensive reading meta-analysis](https://doi.org/10.1002/tesq.157)
- [Sailer & Homner: gamification meta-analysis](https://doi.org/10.1007/s10648-019-09498-w)
- [Duolingo: improving the streak](https://blog.duolingo.com/improving-the-streak/)
- [Schultz: dopamine reward prediction-error signalling](https://www.nature.com/articles/nrn.2015.26)
- [Council of Europe: CEFR descriptors](https://www.coe.int/en/web/common-european-framework-reference-languages/cefr-descriptors)
- [Council of Europe: self-assessment grid](https://rm.coe.int/168069ce6e)

## Daily experience

Confirmed practice budget: 5–30 minutes per day outside teacher lessons. Default to a complete five-minute mission, then offer useful extensions up to 30 minutes. Offer 5, 15, and 30 minutes as quick choices, with the ability to stop or extend at task boundaries. These durations are product choices, not scientifically optimal intervals. The eight-minute sequence below is an illustrative extended mission.

1. Start: a single “Practice today” action. Quiet mode is available when speaking is inconvenient.
2. Recall: two minutes of due items, mixing recent lessons and older knowledge. Begin with an achievable item.
3. Mission: three minutes of a short dialogue or text, followed by a response that accomplishes something.
4. Produce: two minutes speaking or writing about the same situation with fewer hints.
5. Finish: one minute repairing a useful mistake, seeing a concrete gain, and previewing the next mission.

A five-minute session counts as a complete daily commitment. Longer sessions earn additional task rewards without making the short session feel incomplete. Pause and resume automatically in practice mode. A quiet session defers speaking practice and keeps its remaining need visible. Cap the daily queue after missed days so returning does not present a mountain of overdue work. Rotate the main skill across days; do not squeeze all five exam components into every short session. Keep longer unassisted tasks visible as separate evidence needs.

Sample mission from the existing time and verb lessons: arrange a meeting. Hear a proposed time, identify it, then type or say “Nu pot la ora trei. Putem să ne întâlnim la ora patru?” Introduce unfamiliar language before requiring independent recall. Later, vary the time and reason so the task tests transfer. This is an illustrative authored example, not a quotation from the notes.

## Learning engine

- Track each skill separately for recognition, recall, listening, and production. A correct multiple-choice answer does not establish speaking mastery.
- Choose practice from due review, recent teacher material, recurring mistakes, and a manageable new challenge. Tune the balance from use rather than pretending a fixed ratio is optimal.
- Give scaffolding in stages: example, partial response, independent response, new context. Hints count as assisted success.
- Schedule review based on accuracy and assistance; use latency only as a secondary signal because typing and accessibility affect it.
- Teach phrases in contexts as well as word forms: noun with article and plural; verb in a useful sentence; agreement in a complete phrase.
- Romanian input needs easy access to ă, â, î, ș, ț. Distinguish a diacritic issue from a grammar or meaning error. Normalize legacy ş/ţ encodings without erasing real spelling differences.
- Allow legitimate alternate answers. Uncertain automated judgments should be reviewable and should not damage progress. Speech transcription is not a reliable pronunciation score.
- During short drills, correct promptly. During a longer spoken response, let the learner finish, then give focused feedback and a retry.

## Game concept and motivation

Proposed theme: a Romanian journey, with locations unlocked through practical missions. Start with a lightweight map or postcard collection; validate that the theme appeals before investing in art.

| Mechanic | Proposed behavior | Learning connection |
|---|---|---|
| Visible session progress | Small, finite sequence with a satisfying finish | Reduces uncertainty about effort |
| Weekly consistency | Start with a flexible five-days-per-week target; optional daily streak | Supports returning after interruptions |
| XP | Reward substantive attempts, correction, and completed missions; cap easy-repeat rewards | Avoids making easy-item repetition the best strategy |
| Mastery badges | Earned after successful delayed recall and use in a new context | Separates activity from demonstrated learning |
| Story and collections | A new scene or collectible after a mission | Supplies curiosity and variety |
| Choice | Choose between two topics that exercise the same target skill | Gives control without requiring curriculum planning |
| Weekly challenge | A fresh situation with reduced hints | Makes progress tangible |
| Personal bests | Compare new responses to the learner's earlier performance | Rewards competence rather than competition |

Keep feedback warm and specific: “You used the plural correctly without a hint.” Let sound and animations be optional. Preserve earned achievements after missed days. Default to personal progress; public leagues, currencies, energy limits, and complex reward economies are outside the initial scope. These are design choices to validate, not established findings about this user.

## Progression toward B1

B1 requires understanding main points on familiar topics, managing common situations, describing experiences, giving brief reasons, and producing connected text. Build listening, reading, spoken interaction, spoken production, and writing throughout the path.

Start with a gentle diagnostic informed by the current course. Keep teacher-led content as the main sequence, while recording gaps against B1 communicative descriptors. Add A2/B1 material explicitly as new authored content when appropriate. Do not relabel A1 exercises as B1 preparation completed.

Include longer speaking and writing practice weekly; micro-exercises alone cannot demonstrate sustained communication. RoExam task formats and evaluation criteria belong in the first release, with supported practice and timed attempts. Avoid an unvalidated “B1 readiness percentage.” Instead show observed abilities, recent evidence, and skills not yet assessed across all five exam components.

## Content workflow

Import selected material → extract while preserving tables and annotations → identify lesson, prompts, learner answers and corrections → propose skills and exercises → review uncertain content → publish a versioned practice set.

Each exercise retains its source file and location, target skill, level estimate, accepted answers or rubric, explanation, and review status. Distinguish source-supported material from generated extensions. Compare original and teacher versions carefully; deletions and formatting changes are not necessarily corrections. Avoid propagating personal addresses or phone numbers from notes into generated scenarios; use fictional examples.

For the first release, manually curate a small set from two or three lessons. Full document ingestion is a later convenience. Teacher feedback can be manually added; a separate teacher portal is unnecessary initially. A local weekly summary can support the next lesson without automatic sharing.

## First web release

Mobile-friendly web app with Today, Missions, My mistakes, Progress, and Exam practice. Add lesson/source access from exercises and a small content-review area.

Build first:

1. One complete daily session using curated course content.
2. Recall prompts, short listening comprehension, sentence production, and recording/playback with a model response or self-check.
3. Review scheduling, saved attempts, and a mistake queue.
4. A small mission sequence, weekly consistency tracking, and concrete progress feedback.
5. RoExam writing tasks with word counts, the three oral task formats, and a weekly unfamiliar prompt to check transfer. Include grammar/vocabulary as a distinct exam component.

Next: assisted import, better speech feedback validated against teacher judgments, richer stories, and full mock assembly. AI can propose variations and feedback; stable exercises and scheduling should remain usable without generation on every turn. The selected stack is Go, React/TypeScript with Vite, and PostgreSQL, using Docker Compose locally and Helm for existing Google Cloud infrastructure; see [TECH_PLAN.md](TECH_PLAN.md). A local single-user scaffold and configurable external-database/storage chart profiles are implemented. Add separate learner accounts before the first shareable release. Existing Google Cloud compute and Postgres will be reused; exact resources and the authentication provider remain to be selected. Social features can wait. Select a speech provider after validating Romanian audio quality. With December approaching, prioritize useful practice over elaborate game systems.

## Validation before expanding

Run a two-week personal pilot using the curated set. Measure days practiced, time to begin, session completion, and a quick enjoyment rating. Separately measure delayed recall after about a week, success on unfamiliar equivalent prompts, and speaking/writing performance with a consistent rubric and teacher input when available.

Use the pilot to decide whether sessions feel easy to start, whether skills improve beyond repeated prompts, and whether rewards encourage meaningful practice. A single-user pilot cannot establish general causal effectiveness. Compare with a short baseline and adjust one major mechanic at a time where practical.

## Open decisions

- Exact December 2026 exam date and confirmation that the booked session follows the supplied RoExam specification; provider/format reference is now supplied.
- Biggest practice barrier: starting, boredom, forgetting, or speaking discomfort?
- Preferred support language: Hebrew, English, or both?
- Daily commitment confirmed as 5–30 minutes; situations where audio is possible remain open.
- Which playful theme feels appealing: travel, story characters, collecting, or minimal visual progress?

Recommended next planning step: establish available practice time and storyboard the “solve a housing problem” mission in EXAM_PLAN.md, connecting the existing home vocabulary to exam writing and interaction.
