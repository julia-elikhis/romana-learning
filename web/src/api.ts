export type Exercise={id:string;kind:'cloze'|'multiple_choice';prompt:string;options:string[]};
export type Progress={attempts:number;correct:number;practiced:number;available:number;tracked:boolean};
export type Result={correct:boolean;answer:string;explanation:string;sourceQuote?:string;saved:boolean};
export type User={id:string;login:string;name:string;isAdmin:boolean};
export type AuthSession={user:User|null;githubEnabled:boolean};
export type HistoryItem={id:string;exerciseId:string;prompt:string;answer:string;correct:boolean;correctAnswer:string;explanation:string;createdAt:string};
export type Material={id:string;rootId:string;revision:number;title:string;filename:string;kind:string;reviewed:boolean;generated:boolean;text?:string;createdAt:string;draftCount:number;publishedCount:number};
export type Draft=Exercise&{answers:string[];explanation:string;sourceQuote:string;sourceLine:number;status:'draft'|'published'|'rejected'};
export type DraftEdit={kind:Draft['kind'];prompt:string;answers:string[];options:string[];explanation:string};
export type LibraryData={enabled:boolean;apiEnabled:boolean;generator:string;materials:Material[]};
export type MaterialData={material:Material;exercises:Draft[];generator:string};
export type ManagedQuestion=Draft&{materialId:string;materialTitle:string};
export type QuestionPage={questions:ManagedQuestion[];total:number;page:number;pageSize:number};
export type LeaderboardEntry={rank:number;login:string;score:number;isYou:boolean};
export type LeaderboardData={entries:LeaderboardEntry[];you:LeaderboardEntry|null;participants:number;weekStart:string;weekEnd:string};
export async function api<T>(path:string,init?:RequestInit):Promise<T>{
 const response=await fetch(path,init);let data;
 try{data=await response.json()}catch{throw new Error('The server could not complete this request. Please retry.')}
 if(!response.ok)throw new Error(data.error||'Unable to connect');return data;
}
export function json(method:string,body:unknown):RequestInit{return {method,headers:{'Content-Type':'application/json'},body:JSON.stringify(body)}}
