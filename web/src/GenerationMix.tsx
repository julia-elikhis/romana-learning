import {formatNames,type ExerciseKind,type GenerationSummary} from './api';
export function GenerationMix({summary}:{summary:GenerationSummary}) {
 return <section className="generation-mix" aria-label="Generation mix"><h3>Last AI generation</h3><table><thead><tr><th>Format</th><th>Requested</th><th>Created</th></tr></thead><tbody>{(['multiple_choice','cloze','multi_select'] as ExerciseKind[]).map(kind=><tr key={kind}><th>{formatNames[kind]}</th><td>{summary.requested[kind]}</td><td>{summary.created[kind]}</td></tr>)}</tbody></table>{summary.note&&<p className="note">{summary.note}</p>}</section>
}
