// A stable, symmetric identicon rendered locally; no external image requests.
export function Avatar({login}:{login:string}) {
 let hash=2166136261;
 for(const char of login.toLowerCase())hash=Math.imul(hash^char.charCodeAt(0),16777619)>>>0;
 const hue=hash%360,cells=[];
 let bits=hash;
 for(let y=0;y<5;y++)for(let x=0;x<3;x++){
  bits=(Math.imul(bits,1664525)+1013904223)>>>0;
  if(bits&0x80000000){
   cells.push(<rect key={`${x}-${y}`} x={x+1} y={y+1} width="1" height="1"/>);
   if(x<2)cells.push(<rect key={`${4-x}-${y}`} x={5-x} y={y+1} width="1" height="1"/>);
  }
 }
 return <svg className="avatar" viewBox="0 0 7 7" aria-hidden="true" focusable="false"><rect width="7" height="7" rx="1.7" fill={`hsl(${hue} 28% 90%)`}/><g fill={`hsl(${hue} 35% 38%)`}>{cells}</g></svg>
}

export function SignOutIcon() {
 return <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false"><path d="M9 4H5a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h4M14 8l4 4-4 4M8 12h13"/></svg>
}
