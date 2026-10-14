// Stream Go 1.24 parsed trace; never retain the full text expansion.
const {spawn}=require('node:child_process');
const fs=require('node:fs');
const readline=require('node:readline');
const path=require('node:path');
const dir=process.argv[2];
const low=BigInt(process.argv[3]), high=BigInt(process.argv[4]);
const out=fs.createWriteStream(path.join(dir,'timeline.txt'));
const states=new Map(); const long=[];
const p=spawn('go',['tool','trace','-d=parsed',path.join(dir,'runtime.trace')]);
const lines=readline.createInterface({input:p.stdout});
let retain=false;
lines.on('line',line=>{
 const t=line.match(/^M=.* Time=(\d+)/);
 if(t){
  const time=BigInt(t[1]); retain=time>=low&&time<=high;
  const m=line.match(/Resource=(Goroutine|Proc)\((\d+)\).* (\w+)->(\w+)$/);
  if(m){const key=m[1]+m[2];const prev=states.get(key);
   if(prev && time-prev.time>=10000000n && time>=low && prev.time<=high) long.push({resource:key,ms:Number(time-prev.time)/1e6,from:prev.line,to:line});
   states.set(key,{time,line});
  }
 }
 if(retain)out.write(line+'\n');
});
p.stderr.pipe(process.stderr);
p.on('close',code=>{out.end();fs.writeFileSync(path.join(dir,'long-states.json'),JSON.stringify(long,null,2));process.exitCode=code;});
