local B=require "AlchemyBalance"
local D={symbols={},order={},relics={},relicOrder={},balance=B}
D.rarity={"普通","罕见","稀有","传奇"}
D.rents=B.rents;D.spins=B.spins;D.odds=B.symbolOdds

local function symbol(id,name,icon,rarity,value,group,desc,tags,lore)
 D.symbols[id]={id=id,name=name,icon=icon,rarity=rarity,value=value,group=group,desc=desc,tags=tags or {},lore=lore or ""}
 D.order[#D.order+1]=id
end

-- Witch & Occult familiars
symbol("candy","万圣糖果","🍬",1,1,1,"给1币。若相邻有【贪婪南瓜王】，金币直接翻2.5倍！",{},"万圣节小孩最爱的硬糖，甜得很实在，南瓜王的最爱。")
symbol("cat","夜巡黑猫","🐈‍⬛",1,1,1,"吨吨喝掉相邻【幽灵牛奶】，每喝一瓶当场爆赚+30金币！也是文火秘药流的好伙伴。",{witch=true},"通灵的小黑猫，平时爱蹭人，喝饱幽灵牛奶就叼金币回来报答你。")
symbol("milk","幽灵牛奶","🥛",1,1,1,"散发幽光的灵质牛奶；能被黑猫吨吨喝掉爆赚+30金币，也是文火(炉温 0~1)熬制秘药【安眠露】的关键原料！",{},"散发幽光的灵质清凉牛奶，黑猫的最爱，也是熬药良方。")
symbol("owl","守夜鸮","🦉",1,1,1,"每熬制2轮触发一次：本轮额外多发+2金币！",{witch=true},"蹲在药房梁上守夜的老猫头鹰，到点就发辛苦费。")
symbol("crow","厄运乌鸦","🐦‍⬛",1,2,1,"基础给2币；但每熬制3轮会倒扣2币；能被暗夜魔女翻倍翻盘！",{witch=true},"爱报凶信的黑羽乌鸦，魔女平时留着它占卜测算。")
symbol("witch","暗夜魔女","🧙‍♀️",3,2,1,"大魔女显灵！让相邻黑猫、猫头鹰、乌鸦、怨灵、毒苹果产出直接×2！",{human=true,ritual=true},"掌管整间坩埚坊的大魔女，使魔们在她身边干劲全部翻倍。")
symbol("hex","压价紫晶","🔮",2,3,1,"基础给3币；但会让高品质卡更难抽(概率÷1.25)；能被暗夜魔女翻倍。",{witch=true,ritual=true},"压舱用的诅咒紫水晶，虽然值钱，但会压低抽卡欧气。")

-- Surviving ingredients modify the upfront brew; direct collection is always safe.
symbol("toadstool","残次毒蘑菇","🍄",1,1,2,"大火(炉温 ≥3)和剧毒河豚合成主线秘药【熔甲酸】！没被合成留锅里收益+2%、炸锅风险+1%。",{},"带斑点的沼泽毒蕈，丢进高温大火里直冒毒泡。")
symbol("unstable","不稳定火元素","💥",1,2,4,"产出2币；留锅里熬制时收益+4%，但炸锅风险+2%！玩火狂徒必备。",{},"烧得通红的活火星子，越热越赚钱，也越容易炸锅。")
symbol("puffer","剧毒河豚","🐡",2,6,2,"单张暴赚7币！大火(炉温 ≥3)和残次毒蘑菇合成主线秘药【熔甲酸】！留锅收益+10%、炸锅+4%，高风险高回报！",{},"气鼓鼓的致命河豚，全身是毒，高风险伴随极致高回报！")
symbol("antidote","解毒鸟","🐦‍⬛",3,2,1,"防炸神鸟！产出2币；留在锅里熬制时，炸锅风险直接直降-3%！",{},"专吃毒物的小灵鸟，锅里只要有它在，大火熬药心里就不慌。")

-- Herbal & Fruit
symbol("rose","曼陀罗花","🥀",1,2,2,"文火(炉温 0~1)和幽灵牛奶合成主线秘药【安眠露】！相邻食蜜鬼蝠×2、魔法夜雨×2、猩红血月×3.0连乘翻倍暴赚！",{},"冷水慢熬才肯开花的幽香魔花，闻一口就让人甜甜睡去。")
symbol("bee","食蜜鬼蝠","🐝",1,2,2,"勤劳小蝠！挨着的曼陀罗花产出×2，场上每有一朵花自己再+1币！",{},"专门采食魔花蜜的小夜蝠，身边只要有花就拼命干活。")
symbol("rain","魔法夜雨","🌧️",2,2,2,"旁边的曼陀罗花产出×2；且让锅里所有南瓜种子发芽率暴涨+40%！",{},"蕴含纯净生机的夜雨，催开鲜花，更能唤醒沉睡的种子。")
symbol("sun","猩红血月","🩸",3,3,2,"血月高悬！相邻曼陀罗花产出×3.0（一朵花最多吃三张血月，递减叠乘 ×3.0/×2.0/×1.4）！南瓜种子发芽率暴涨+40%！",{},"一轮巨大的绯红圆月，照拂之下草木疯狂滋长。")
symbol("seed","南瓜种子","🌱",1,2,2,"上场有30%几率随机发芽成长为高阶水果；若掉在最底下一排或场上有【南瓜果王】必定发芽！",{},"泡在药汤里的南瓜小苗，说发芽就发芽，充满无限惊喜。")
symbol("eye","眼球软糖","👁️",1,2,2,"水果类；口感酸甜Q弹，是【南瓜果王】最爱吃的吞噬点心！",{fruit=true},"晶莹剔透的眼珠形状软糖，看着吓人，嚼起来酸甜爆浆。")
symbol("banana","梦网丝","🧵",1,2,2,"灵巧蚕丝；每累计上场2次，织出一张专克恶灵的【封印符】加入坩埚袋！",{},"灵动的银白蚕丝，能交织出辟邪道符。")
symbol("peel","封印符","📜",1,2,2,"驱魔道符！挨着【潜行梦魇】任意炉温直接炼成主线秘药【织梦雾】并奖150币；挨着【贪念残魂】当场将其超度为【净化炉灰】并奖32币！",{},"画满辟邪咒文的驱魔符纸，贴在恶灵身上立刻净化超度。")
symbol("apple","毒苹果","🍏",3,3,2,"高阶水果；给3币；受暗夜魔女翻倍成6币！南瓜果王吃下它能暴涨2点永久成长！",{fruit=true,witch=true},"毒汁浸透的红苹果，表面光鲜诱人，吃一口昏睡整天。")
symbol("pumpkin","巨型南瓜","🎃",4,6,2,"传奇大南瓜！场上每多一个巨型南瓜，每个都额外+1币（同种加成最多+6）！越多越爽！",{fruit=true},"万圣节极品金南瓜，凑成一堆就是一整片丰收的南瓜田。")
symbol("fruitking","南瓜果王","🎃",3,5,2,"吞噬大胃王！一口吃掉相邻所有普通/罕见/稀有水果，每吃一个当场爆现金币且永久成长+1！每次上场御赐1颗南瓜种子，使全场水果产出额外+3币！",{human=true},"无限成长的南瓜巨无霸，只要水果管够，一路吃到单刷死神！")
symbol("voidfruit","虚空鬼果","🕳️",2,0,2,"身边每有一个空格子就+1币；若周围全被填满则自毁献祭，一次性给+6币！",{fruit=true,void=true},"来自虚空的空心黑果，周围越空旷，它吸取的能量就越多。")
symbol("coconut","骷髅椰果","🥥",2,2,2,"硬壳水果；被敲碎或自毁时，直接裂解出2个【半个骷髅椰果】进袋！",{fruit=true,monkey=true},"硬得像石头的骷髅椰子，砸开里面正好是两瓣甜椰肉。")
symbol("coconut_half","半个骷髅椰果","🥥",2,3,2,"水果类；产出3币；是南瓜果王与沼泽巨魔最心仪的高性价比零食！",{fruit=true,monkey=true},"香气扑鼻的半边椰子，喂给大怪兽能换回丰厚金币。")
symbol("orange","幽灵橘","🍊",2,3,2,"水果类；产出3币；半透明的酸甜橘子，留着喂果王能稳步成长！",{fruit=true},"泛着荧荧绿光的酸幽灵橘，剥开一瓣满嘴果香。")
symbol("peach","留种桃","🍑",2,3,2,"水果类；产出3币；被吃掉或销毁时，将1颗【南瓜种子】塞进袋中继续发芽！",{fruit=true},"鲜嫩多汁的仙桃，吃完果肉留下的桃核又是一条新生命。")
symbol("pear","魔法梨","🍐",3,1,2,"成长神梨！每次被周围任何效果加成一次，自己永久基础金币+1（最多+60）！越养越强！",{fruit=true,growOnBuff=true},"吸收魔力就疯狂拔高的魔法金梨，备受照顾的小宝贝。")
symbol("strawberry","血莓","🍓",3,3,2,"成对草莓；只要场上同时有2个以上血莓，每个血莓立刻额外+1币！",{fruit=true},"只有成双成对结伴生长时才香气四溢的魔法野莓。")

-- Graveyard & Undead
symbol("cultist","狂热邪教徒","🕯️",1,0,3,"信徒聚会！场上每多一个信徒自己就+1币；同屏3个以上时每个再额外+1！",{human=true,ritual=true},"身披黑袍的狂热信徒，聚在一起念咒，人越多财气越足。")
symbol("urn","封魂骨瓮","⚱️",1,1,3,"大火(炉温≥3)或被敲碎时破裂，释放出1只高额产出的【游荡怨灵】！",{},"密封的千年骨灰陶瓮，一经高温炙烤就砰然开封。")
symbol("bigurn","大封魂棺","🪦",2,2,3,"大号石棺；大火(炉温 ≥3)或被敲碎时，释放出2只【游荡怨灵】！",{},"沉重的黑曜石棺木，敲碎后爆出双倍怨灵。")
symbol("spirit","游荡怨灵","👻",3,6,3,"单次给6币！若飘在最顶上一排产出直接×2！累计熬制3次后功德圆满升华飞升。",{witch=true},"飘忽不定的小幽灵，最喜欢飞到锅顶看热闹，放顶排直接翻倍。")
symbol("digger","掘墓怪人","⛏️",2,2,3,"专业砸罐人！敲碎相邻的所有骨瓮和石棺，每砸一个当场暴赚+8金币！",{human=true},"扛着大铁铲的熟练矿工，看见瓶瓶罐罐上去就是一铲子。")
symbol("necro","死灵术士","💀",3,1,3,"献祭大师！献祭掉相邻的所有人类材料，每个直接给你爆出+18金币巨款！",{human=true},"高冷深沉的亡灵法师，手一抬活人全变成叮当响的金币。")

-- Mineral & Alchemy Chests
symbol("key","生锈铁钥匙","🗝️",1,1,4,"开箱必备！打开1个相邻宝箱提取巨款金币，用完折断销毁。",{},"一把普通的旧钥匙，只要插进箱孔就能开启财富。")
symbol("magic_key","万能魔钥","✨",3,2,4,"神级金钥匙！打开相邻宝箱，并将开箱爆出的金币直接翻2.5倍！",{},"镀金魔法魔钥，经它开出来的箱子，财宝全部翻番爆满。")
symbol("lockbox","带锁木棺","⚰️",1,1,4,"藏宝木箱；被钥匙打开或自毁时，当场爆出10金币(魔钥开给25币)！",{chest=10},"用粗铁条钉紧的实木宝盒，里面藏着不少私房钱。")
symbol("safe","恶魔保险箱","🧰",2,1,4,"精铁保险箱；被钥匙打开或自毁时，直接大爆18金币(魔钥开给45币)！",{chest=18},"沉甸甸的恶魔合金保险柜，砸开就是一笔横财。")
symbol("chest","骷髅宝箱","🎁",3,2,4,"豪华秘宝箱！被钥匙开启时狂爆30金币！魔钥开更是直接给75币！",{chest=30},"雕刻骷髅华丽花纹的重金宝箱，开一个能抵一整轮租金。")
symbol("mega","巨型遗迹箱","🏛️",4,3,4,"传奇怪兽箱！普通钥匙开给60币，万能魔钥开直接给150金币巨款！",{chest=60},"远古遗迹深处的终极巨型宝库，开启一刻金光漫天！")
symbol("ore","冥界矿石","🪨",1,2,4,"大火(炉温≥3)或被火蜥蜴敲碎时裂开，必爆出一颗闪烁的珍贵宝石！",{ore=1},"黑黢黢的地底原石，皮壳一裂就是温润通透的宝石。")
symbol("bigore","巨型矿脉","⛰️",2,2,4,"富集矿脉！大火(炉温≥3)或被敲碎时，一口气爆出2颗珍稀宝石！",{ore=2},"连绵的晶石母矿，高温一烧瞬间爆成两颗纯净宝石。")
symbol("miner","炼金火蜥蜴","🦎",1,1,4,"碎矿能手！熔化相邻的所有矿石与矿脉，每熔一个奖+120金币并掉落宝石！",{human=true},"肚子里燃着熊熊烈火的炼金蜥蜴，最爱把矿石烧化取乐。")
symbol("geo","科学怪人","🧟",3,2,4,"吃宝石狂人！见身边的鬼火和各类宝石就吞，每吃一颗自己永久基础金币+1（最多+150）！",{human=true},"拼合出的怪力巨人，咔嚓咔嚓嚼碎宝石当口粮，越吃越强。")
symbol("fire","小鬼火","🔥",1,1,4,"宝石类；基础给1币，闪烁微光，可作为科学怪人的优质零食。",{gem=true,geo=true},"汤面上跳跃的幽灵小火星，看着温暖，其实一点也不烫手。")
symbol("pebble","闪光石","✨",1,1,4,"欧皇转运石！只要在场上，本轮抽卡的高品质卡爆率直接提升1.15倍！",{gem=true,geo=true},"自带欧气的闪闪亮石，揣在兜里抽卡运气直线起飞。")
symbol("sapphire","蓝魂石","🔷",2,5,4,"宝石类；给5币；深邃幽蓝的晶石，也是科学怪人最爱嚼的果冻糖。",{gem=true,geo=true},"凝结深海寒气的蓝宝石，握在手里凉爽沁心。")
symbol("voidstone","虚空石","🕳️",2,0,4,"宝石类；边上每有一个空格子就+1币；边上全满则自毁献祭大赚+6币！",{gem=true,void=true},"来自暗空的吸灵黑石，周围越空旷它越精神。")
symbol("amethyst","紫魔晶","🔮",3,4,4,"成长型宝石！只要被场上任何伙伴增益一次，自己永久基础金币+1！越养越猛！",{gem=true,growOnBuff=true},"魔导性极强的紫色晶钻，越受照顾绽放得越耀眼。")
symbol("emerald","绿幽石","💚",3,6,4,"成对宝石；只要场上有2颗或更多绿幽石，每颗立刻额外+1币！",{gem=true},"晶莹剔透的翡翠石，同伴越多光芒越耀眼。")
symbol("ruby","血红宝","🔴",3,6,4,"成对宝石；只要场上有2颗或更多血红宝，每颗立刻额外+1币！",{gem=true},"如炽热鲜血般的红宝石，凑在一起互相辉映。")
symbol("diamond","灵魂钻石","💎",4,6,4,"传奇极品宝石！场上每多一颗灵魂钻石，每颗都额外+1币（同种加成最多+6）！全屏钻石直接暴富！",{gem=true},"世间最硬最耀眼的灵魂钻石，多拿几颗全场金币喷涌！")
symbol("midas","贪婪南瓜王","👑",3,1,4,"糖果流核心！每次上场免费送一颗糖果进袋；相邻糖果产出直接翻2.5倍！",{human=true},"头顶纯金王冠的南瓜霸主，走到哪糖果就撒到哪。")

-- Halloween Party & Cards
symbol("die3","三面骨骰","🎲",1,1,5,"趣味骰子；随机投出1~3点拿对应金币；搭配【看骰镜】必定投出3点顶格！",{die=3},"三个面的兽骨骰子，掷出几点当场拿几块金币。")
symbol("die5","五面魔骰","🎲",2,1,5,"高阶魔骰；随机投出1~5点拿对应金币；搭配【看骰镜】必定投出5点顶格！",{die=5},"五个面的彩光魔骰，点数更大，摇起来更刺激。")
symbol("gambler","赌命小鬼","🎲",1,1,5,"小储蓄罐！每次上场存+3币；只要场上任何骰子掷出1点，当场砸罐兑现全部金币！",{human=true},"抱着存钱罐的贪财小鬼，平时省吃俭用，砸罐一瞬间暴富。")
symbol("joker","炼金法阵","⚗️",3,3,5,"法阵核心！让相邻所有的地、风、水、火魔晶产出直接×2.2！强化全场魔晶共鸣！",{},"悬浮于坩埚上空的六芒星炼金法阵，引动四方元素共鸣。")
symbol("spade","地之魔晶","🟤",2,1,5,"土系魔晶；身边每挨着一颗其他属性魔晶，大家都额外+3金币！",{element="earth"},"沉稳坚固的大地晶石，与各系魔晶相邻共鸣生财。")
symbol("club","风之魔晶","🌪️",2,1,5,"风系魔晶；身边每挨着一颗其他属性魔晶，大家都额外+3金币！",{element="air"},"呼啸流转的狂风晶石，与各系魔晶相邻共鸣生财。")
symbol("heart","火之魔晶","🔥",2,1,5,"火系魔晶；身边魔晶各+3币；若在【大火(炉温 ≥3)】下熬，自己再暴涨+10币！",{element="fire"},"灼热燃烧的赤火晶石，大火烈焰中魔力暴增。")
symbol("diamond_suit","水之魔晶","💧",2,1,5,"水系魔晶；身边魔晶各+3币；若在【文火(炉温 0~1)】下熬，自己再暴涨+10币！",{element="water"},"柔和澄澈的碧水晶石，文火慢炖中魔力暴增。")
symbol("dame","血族女王","🧛‍♀️",3,2,5,"女王降临！使相邻宝石产出×1.7，同一颗宝石最多吃三位女王（×1.7/×1.3/×1.15）；文火(炉温 0~1)时将月露圣杯炼化为高阶产物【血月精华】并暴赚35币！",{human=true},"高贵的血族女王，就爱品尝盛在圣杯中的月露神酿。")
symbol("martini","月露圣杯","🏺",3,3,5,"高级灵药圣杯；文火(炉温 0~1)且挨着血族女王时，会被淬炼成高阶产物【血月精华】并暴奖35金币！",{},"纯银打造的秘银圣杯，盛满在月夜淬炼出的纯净灵药。")
symbol("thief","潜行梦魇","🌑",2,2,5,"身怀巨款的幽暗恶念；挨着【封印符】任意炉温直接合成为主线秘药【织梦雾】并当场触发恶念超度爆奖+150金币！",{},"从深渊噩梦中逃出的无形梦魇，一旦被封印符贴住即超度为织梦灵雾。")
symbol("troll","沼泽巨魔","👹",1,1,2,"吞噬相邻骷髅椰果和半个骷髅椰果；每个按其价值的5倍给金币。",{},"大肚沼泽怪，见椰果就吞，翻五倍还你。")
symbol("billionaire","贪念残魂","👻",2,0,5,"平时不产出；拥有遗物【净化烛台】时会被当场超度，化为超值圣洁材料【净化炉灰】并产出32币！",{},"执迷不悟的贪财残魂，唯有圣洁烛火能超度其执念。")
symbol("rabbit","月夜玉兔","🐇",2,1,2,"陪伴型小宠！累计在场上熬制8次后，永久基础产出直接升级+2金币！",{},"小白兔，守够日子就长大。")


-- Neutral Mercenaries (Factionless Standalone Cards - Inspired by Auto Chess)
symbol("ronin","孤狼浪人","🗡️",2,9,6,"孤傲剑客；若周围8邻格没有任何其他人类或使魔，自身独享10金币全额产出；若身边有人类/使魔同行，产出骤降为2币。",{human=true,ronin=true},"行走于暗夜的独行浪客，从不与任何人拉帮结派，唯有一剑孤行。")
symbol("taotie","吞金饕餮","🐊",3,2,6,"大胃神兽！每轮随机吃掉相邻1张非主线普通卡(普通/罕见)；吃掉时当场返还其价值×2+6金币，并使自己永久基础金币+1！",{taotie=true},"上古凶兽饕餮，见什么吞什么，只要不是致命毒药，任何废弃材料进了它肚子都能化作金币。")
symbol("colossus","魔导巨像","🗿",3,11,6,"重装造物；自身拥有14金币超高基础产出！但庞大身躯产生重压，使相邻所有卡牌基础产出-1金币(最低减至0)。",{colossus=true},"古代炼金师打造的重型魔能巨像，虽然威力无穷，但一脚踩下整片地表都在剧烈颤抖。")
symbol("astrologer","占星魔女","🔮",3,1,6,"星象大师；给4币；只要在场上，本轮抽卡获得罕见/稀有/传奇卡的好运气提升1.5倍；每次熬制有50%几率赠送1次免费重抽(Reroll)！",{witch=true,human=true,ritual=true},"仰望星空的占星魔女，能洞察命运长河的流向，随手拨动星轨便能逆天改命。")
symbol("berserker","浴血角斗士","🪓",2,2,6,"逆境战狂；平时给3币；当处于欠租危机(持有金币＜当夜房租的50%)时，当场进入狂暴状态，自身产出暴涨+12币(合计15币)！",{human=true},"生死边缘才能觉醒真正力量的狂战士，越是绝境，手中的双斧挥动得越发迅猛致命。")

-- Witch Relics (Original Balance)
local function relic(id,name,icon,rarity,desc,lore)
 D.relics[id]={id=id,name=name,icon=icon,rarity=rarity,desc=desc,lore=lore or ""}
 D.relicOrder[#D.relicOrder+1]=id
end

relic("paw","黑猫肉球印","🐾",1,"场上每有一只黑猫，抽到罕见/稀有/传奇卡的好运气提升1.25倍！","软乎乎的招财肉垫，踩出满满的欧皇好运气。")
relic("pizza","幽灵小鱼干","🍕",1,"所有黑猫每轮基础产出+8币；喝掉幽灵牛奶依然能狂赚额外金币！","磷火炭烤的秘制小鱼干，黑猫闻了口水直流。")
relic("pick","精钢炼金镐","⛏️",1,"矿石被敲碎开采时，每次额外多拿+40金币！","矮人锻造的破山硬镐，专砸各类顽固矿石。")
relic("glasses","真视魔镜","🧐",3,"命运魔镜！骰子不再随缘乱滚：三面必投3点，五面必投5点！","能看透命运轨迹的占卜镜片，让魔骰永远掷出顶格点数。")
relic("bible","古神秘典","📜",2,"狂热邪教徒、暗夜魔女、压价紫晶每张产出额外+1金币！","泛黄的手抄古老教典，信徒与魔女一读就格外兴奋。")
relic("hat","女巫法帽","🧙‍♀️",3,"血月光环！场上有【猩红血月】时，全场曼陀罗花哪怕不相邻也能吃满血月连乘翻倍，南瓜种子也吃满发芽率！","缀满银月星芒的尖顶巫师帽，能引动猩红月华笼罩整座坩埚。")
relic("guillotine","净化烛台","🕯️",1,"自动超度锅里所有的贪念残魂，当场将其转化为高价值的净化炉灰！","一小撮静静燃烧的神圣烛火，专治执迷不悟的贪财鬼。")
relic("fertilizer","生机催化剂","🧪",2,"强效化肥！锅里的种子必定发芽，而且专长稀有以上的高阶果实！","绿油油的浓缩养料，种子沾一点就疯长成果王。")
relic("magnet","星辰吸铁石","🧲",4,"高阶好卡抽卡爆率暴涨3倍！玉兔产出直接翻3倍！","流星碎片打造的磁石，专把世间最好的宝物吸过来。")
relic("candle","安息魂烛","🕯️",1,"所有扣钱的效果少扣1币；每轮摇号熬制完全免费！","散发宁静白光的香烛，专治各种乱七八糟的扣费坏毛病。")
relic("broom","飞天扫帚","🧹",2,"每转8次就白送一张【驱魔符】，助你轻松精简净化卡组！","爱干净的魔女扫帚，随手一挥就帮你扫掉废卡。")
relic("pig","黑胡椒研磨器","💀",1,"锅里每有一张材料被销毁或吞噬，当场多奖+1金币！","撒一撮秘制研磨调料，给每一次药材融合提鲜加钱。")
relic("pressure","紫铜高压锅盖","🫕",2,"熬制时的炸锅风险直接下压2个百分点，绝不爆锅！","严丝合缝的厚重铜盖，再大的高温蒸汽也稳稳压住。")
relic("outlet","双头分流管","⚗️",3,"险中求富！炸锅风险≥30%时熬制成功，整锅收益再额外暴增+25%！","临界状态下自动分压的泄流管，富贵险中求的利器。")
relic("frost_dew","寒霜露","❄️",1,"开锅助剂：炸锅风险直降-6%，收益微降5%；保命稳健之选！","封着一口凛冽冬夜霜气的玉瓶，一滴就能快速降温。")
relic("dragon_dust","龙息粉","🔥",2,"开锅助剂：收益暴涨+35%，炸锅风险+8%；赌怪暴富首选！","狂暴巨龙鳞片磨成的赤红火药，让整锅汤瞬间沸腾暴赚。")
relic("safety_seal","保温符","🛡️",3,"开锅助剂：哪怕不小心炸锅，也能保住并退还50%基础金币！","画着守护结界的符纸，万一炸锅也能保住大半心血。")

-- 万能卡：不占流派，只有两个用途——全场加成 + 折算仪式门槛。
symbol("omni_cauldron","万灵坩埚","🍯",3,3,0,"万能核心！不属任何流派，但让全场产出+12%（可叠加，上限×3）；交租时也算 1 张同流派，帮主流派凑仪式门槛。",{universal=true},"把所有流派熬进同一锅的老坩埚，谁的味儿都沾一点。")
symbol("omni_pact","使徒誓约","📜",2,2,0,"万能契约！不属任何流派，让全场产出+12%（可叠加）；交租时也算 1 张同流派，混合流派同样能吃到仪式复利。",{universal=true},"与使徒私下签下的契约，翻开来全是看不懂的条款。")
symbol("nine_lives_cat","九命灵猫","🐱",4,8,1,"【使魔传奇】同场使魔与女巫收益×2；若当夜坩埚炸锅，消耗自身1条命并免除炸锅惩罚（上限9次）。",{witch=true,cat=true},"拥有九条命的通灵古猫，是女巫最忠实的守护灵。")
symbol("lich_emperor","冥魂大巫妖","👑",4,8,3,"【死灵传奇】场上凡人/骨灰被超度或裂解时自身永久+2；全场死灵流材料结算倍率额外×2！",{human=true,witch=true,undead=true},"掌控生死界限的死灵帝王，能将每一粒骨灰转化为无尽的幽冥之力。")
symbol("hydra","万毒九头蛇","🐍",4,9,2,"【毒素传奇】高温大火与文火均不消散；同场每有1个毒素材料，全场毒素材料基础点数+4，且获得×1.5剧烈扩散！",{poison=true,growOnBuff=true},"剧毒之渊的远古蛇神，毒素越浓烈，其身躯越发庞大。")
symbol("archmage","全能大魔导","🧙",4,8,5,"【奥术传奇】同时掌握全系魔法。若场上凑齐水、火、风、地四种元素魔晶，所有元素材料基础值+8，全场最终倍率额外×2.5！",{element="all",human=true},"将扑克法阵推演至奥术巅峰的魔导大师。")
symbol("philosophers_stone","贤者之石","💎",4,12,0,"【中立传奇】炼金术最高杰作。完美平衡：若场上没有任何单一流派超过3张卡牌，全场材料收益×2.5！且每熬成一锅药，获赠1张免费驱魔符。",{universal=true},"超越一切流派拘束的终极物质，唯有心境最均衡的炼金术士方能驾驭。")
symbol("abyss_heart","深渊核心","🖤",4,9,1,"【仪式传奇】吞噬相邻的信徒或凡人，每次吞噬获得40币并直接为本流派永久累积1层流派仪式。",{ritual=true},"自深渊裂缝中跳动的心脏，贪婪地渴求着崇拜者的献祭。")
symbol("alchemy_scale","炼金天平","⚖️",2,4,0,"【中立运营】若场上流派种类数≥3，使全场所有卡牌基础值+2；且当手牌臃肿为0时，全场收益额外×1.3。",{universal=true},"绝对公正的炼金天平，奖励不偏激、不臃肿的运营构筑。")
D.seedPool={"voidfruit","banana","eye","coconut","rose","pear","orange","peach","apple","strawberry","pumpkin"}
D.orePool={"voidstone","amethyst","fire","pebble","sapphire","emerald","ruby","diamond"}
D.groups={"使魔夜巫","草木灵果","墓园亡灵","炼金晶矿","万圣狂欢"}
-- Preserve stable IDs for save compatibility while replacing their old fantasy and rules.
local function redesign(id,name,icon,desc,tags,lore)
 local s=D.symbols[id];s.name=name;s.icon=icon;s.desc=desc;s.tags=tags or s.tags;s.lore=lore or s.lore or ""
end
redesign("milk","幽灵牛奶","🥛","文火(炉温 0~1)和曼陀罗花熬成主线秘药【安眠露】；还能被黑猫吨吨喝掉爆赚+30金币！",nil,"散发幽光的灵质清凉牛奶，黑猫的最爱，也是熬药良方。")
redesign("cat","夜巡黑猫","🐈‍⬛","吨吨喝掉相邻【幽灵牛奶】，每喝一瓶当场爆赚+30金币！也是文火秘药流的好伙伴。",nil,"通灵的小黑猫，平时爱蹭人，喝饱牛奶就叼金币回来报答你。")
redesign("rose","曼陀罗花","🥀","文火(炉温 0~1)和幽灵牛奶熬成主线秘药【安眠露】！相邻食蜜鬼蝠×2、魔法夜雨×2、猩红血月×3.0（一朵花最多吃三张血月，递减叠乘，总倍率不设上限）连乘翻倍暴赚！",nil,"冷水慢熬才肯开花的幽香魔花，闻一口就让人甜甜睡去。")
redesign("toadstool","残次毒蘑菇","🍄","大火(炉温 ≥3)和剧毒河豚熬制合成主线秘药【熔甲酸】！没被合成留锅里收益+2%、炸锅风险+1%。",nil,"带斑点的野蕈，丢进高温大火里直冒毒泡。")
redesign("puffer","剧毒河豚","🐡","大火(炉温 ≥3)和残次毒蘑菇熬制合成主线秘药【熔甲酸】！留锅收益+10%、炸锅风险+4%，高风险高回报！",nil,"气鼓鼓的致命河豚，全身是毒，高风险伴随极致高回报！")
redesign("ore","冥界矿石","🪨","大火(炉温 ≥3)或被敲碎时裂开，必爆出1颗闪亮的珍贵宝石！",nil,"黑黢黢的地底原石，皮壳一裂就是温润通透的宝石。")
redesign("bigore","巨型矿脉","⛰️","大火(炉温 ≥3)或被敲碎时裂开，一口气爆出2颗珍稀宝石！",nil,"富含晶石的母矿，高温一烧瞬间爆成两颗纯净宝石。")
redesign("urn","封魂骨瓮","⚱️","大火(炉温 ≥3)或被敲碎时碎裂，释放出1只高额产出的【游荡怨灵】！",nil,"密封的千年古陶瓮，高温炙烤下砰然开封。")
redesign("bigurn","大封魂棺","🪦","大号石棺；大火(炉温 ≥3)或被敲碎时，释放出2只【游荡怨灵】！",nil,"沉重的黑曜石棺木，敲碎后爆出双倍怨灵。")
redesign("banana","梦网丝","🧵","灵巧蚕丝；每累计上场2次，织出一张专克恶灵的【封印符】加入坩埚袋！",{})
redesign("troll","沼泽巨魔","👹","一口闷！吞掉相邻所有椰果和半椰果，按其价值的5倍返还高额金币！",{})
redesign("peel","封印符","📜","驱魔道符！挨着【潜行梦魇】任意炉温直接炼成主线秘药【织梦雾】并奖150币；挨着【贪念残魂】当场将其超度为【净化炉灰】并奖32币！",{})
redesign("thief","潜行梦魇","🌑","身怀巨款的幽暗恶念；挨着【封印符】任意炉温直接合成为主线秘药【织梦雾】并当场触发恶念超度爆奖+150金币！",{})
redesign("joker","炼金法阵","⚗️","法阵核心！让相邻所有的地、风、水、火魔晶产出直接×2.2！",{})
redesign("spade","地之魔晶","🟤","土系魔晶；身边每挨着一颗其他属性魔晶，大家都额外+3金币！",{element="earth"})
redesign("club","风之魔晶","🌪️","风系魔晶；身边每挨着一颗其他属性魔晶，大家都额外+3金币！",{element="air"})
redesign("heart","火之魔晶","🔥","火系魔晶；身边魔晶各+3币；若在【大火(炉温 ≥3)】下熬，自己再暴涨+10币！",{element="fire"})
redesign("diamond_suit","水之魔晶","💧","水系魔晶；身边魔晶各+3币；若在【文火(炉温 0~1)】下熬，自己再暴涨+10币！",{element="water"})
redesign("dame","血族女王","🧛‍♀️","女王降临！使相邻宝石产出×1.7，同一颗宝石最多吃三位女王（×1.7/×1.3/×1.15）；文火(炉温 0~1)时将月露圣杯炼化为高阶产物【血月精华】并暴赚35币！",{human=true})
redesign("martini","月露圣杯","🏺","高级灵药圣杯；文火(炉温 0~1)且挨着血族女王时，会被淬炼成高阶产物【血月精华】并暴奖35金币！",{})
redesign("billionaire","贪念残魂","👻","平时不产出；拥有遗物【净化烛台】时会被当场超度，化为超值圣洁材料【净化炉灰】并产出32币！",{})
D.relics.guillotine.name="净化烛台";D.relics.guillotine.icon="🕯️";D.relics.guillotine.desc="自动超度锅里所有的贪念残魂，当场将其转化为高价值的净化炉灰！";D.relics.guillotine.lore="一小撮静静燃烧的神圣烛火，专治执迷不悟的贪财鬼。"
D.relics.pizza.desc="所有黑猫每轮基础产出+8币；喝掉幽灵牛奶依然能狂赚额外金币！"
symbol("dream_potion","安眠露","🧪",2,5,2,"【主线秘药 1/4】文火(炉温 0~1)由曼陀罗花 + 幽灵牛奶熬制合成；每轮稳定给5币，能永久破除第3夜使徒与死神的【恐惧护盾】！",{},"冷火文火慢熬出的安眠灵药，第3夜使徒恐惧白骑闻了当场打瞌睡倒地。")
symbol("poison_crystal","熔甲酸","💠",3,13,4,"【主线秘药 2/4】大火(炉温 ≥3)由残次毒蘑菇 + 剧毒河豚熬制合成；基础收益+8%、炸锅风险+3%，能永久破除第6夜使徒与死神的【镰甲护盾】！",{gem=true},"烈火熬成的极腐强酸，能彻底融化第6夜使徒镰甲红骑的坚硬死灵重铠。")
symbol("dream_essence","织梦雾","🌙",2,12,3,"【主线秘药 3/4】由封印符 + 潜行梦魇在任意炉温下炼化合成；每轮稳定给13币，使全场所有灵异与使魔产出各+1币，能永久看破第9夜使徒与死神的【幻影护盾】！",{},"封印梦魇恶念炼出的织梦灵雾，专破第9夜使徒幻影黑骑的虚妄分身。")
symbol("blood_essence","血月精华","🩸",3,7,3,"【高阶炼金产物】文火(炉温 0~1)由血族女王转化月露圣杯炼化得到；每轮单张直接产出7金币！",{},"纯度极高的血族圣血秘宝，散发着诱人的猩红微光。")
symbol("purified_ash","净化炉灰","✨",2,5,3,"【圣洁炼金产物】贪欲恶灵或游荡怨灵被超度净化后留下的纯净炉灰；单张稳定产出5金币，纯净无害！",{},"贪念被神圣火焰焚烧殆尽后剩下的纯净灰烬。")
for _=1,5 do table.remove(D.order) end -- products are crafted, not offered directly
D.seedPool={"voidfruit","eye","coconut","rose","pear","orange","peach","apple","strawberry","pumpkin"}
D.groups[5]="元素仪式"
D.groups[6]="流浪雇佣"
-- Rarity value curve: commons stay low, rare cards carry the build.
do
 local rvb=B.rarityValueBonus
 if rvb then
  for _,s in pairs(D.symbols) do if s.value>=1 then s.value=s.value+(rvb[s.rarity] or 0) end end
 end
end
-- Describe the extra reaction reward separately from the cat's own production.
D.symbols.pear.desc="每接受一名伙伴的正向加成，永久基础产出+"..B.rules.growth.."币（最多+"..((B.rules.growCap and B.rules.growCap.pear) or 20).."）；同一来源每轮最多触发一次，包括果王的全场水果加成。"
D.symbols.amethyst.desc="接受伙伴增益时永久基础产出+"..B.rules.growth.."币（最多+"..((B.rules.growCap and B.rules.growCap.amethyst) or 10).."）；同一来源每轮最多触发一次，血族女王的宝石加成可以触发。"
D.symbols.fruitking.desc="吞噬相邻普通/罕见/稀有水果，每个分别永久成长+"..((B.rules.fruitGain and B.rules.fruitGain[2]) or 1).."币，并获得吞噬金币；同场其他水果各+5币，自己每有一颗水果额外+3币。"
D.symbols.rabbit.desc="累计上场熬制"..B.rules.rabbitAfter.."次后，永久成长+"..B.rules.rabbitBonus.."币，从下一次产出开始生效。"
D.symbols.cat.desc="饮用相邻【"..D.symbols.milk.name.."】，每瓶额外增加"..B.rules.milk.."金币；黑猫的基础产出另计，也可接受相邻魔女加成。"
D.symbols.omni_cauldron.desc="【万能】不属任何流派；在场时全场产出+"..math.floor(B.universal.mulStep*100+0.5).."%，万能加成可叠加至×"..B.universal.mulCap.."。若场上每个流派至多3张，万能加成再×1.25。交租时为每个流派的仪式门槛各计"..B.universal.riteCredit.."张。"
D.symbols.omni_pact.desc=D.symbols.omni_cauldron.desc
D.symbols.alchemy_scale.desc="【万能】在场时提供万能加成和仪式门槛计数。场上至少有3种流派时，自身基础产出+2；牌袋不超过21张时，自身收益×1.3。"
D.symbols.philosophers_stone.desc="【万能】在场时提供万能加成和仪式门槛计数。场上每个流派至多3张且牌袋不超过21张时，自身收益×2.5；否则基础产出变为2。牌袋中有贤者之石时，每次成功熬药获得1张驱魔符，多张不叠加。"
D.symbols.abyss_heart.desc="吞噬相邻信徒或凡人，每次获得40币，自身永久成长+2币，并额外获得20币。"
D.symbols.milk.desc="文火（炉温 0～1）时与相邻【曼陀罗花】炼成【安眠露】；被相邻黑猫饮用时额外增加"..B.rules.milk.."金币。"
D.relics.pizza.desc="所有黑猫每轮基础产出+"..B.rules.pizza.."币；饮用幽灵牛奶带来的额外收益另外结算。"
D.relics.fertilizer.desc="每件使南瓜种子的发芽率提高"..math.floor(B.rules.fertilizerBoost*100+0.5).."个百分点；种子成功发芽时，产物至少为稀有品质。"
local ess=B.rules.essenceBonus or 100
D.symbols.peel.desc="与相邻【潜行梦魇】在任意炉温下合成【织梦雾】，额外获得"..ess.."金币；与相邻【贪念残魂】合成【净化炉灰】，额外获得"..B.rules.billionaire.."金币。"
D.symbols.thief.desc="与相邻【封印符】在任意炉温下合成【织梦雾】，额外获得"..ess.."金币。"
D.symbols.billionaire.desc="自身不产出；与相邻【封印符】合成【净化炉灰】，额外获得"..B.rules.billionaire.."金币；持有【净化烛台】时自动转化为【净化炉灰】。"
-- 压平卡面数值：高值卡不再同时是“最强引擎”，把强度让给联动/成长/倍率。
-- 目标区间 3~8（原本 1~14），这样“挑最大数字”不再等于“选最强卡”。
do
 local cap={colossus=8,pumpkin=7,diamond=7,ronin=6,spirit=6,emerald=6,ruby=6,fruitking=6,puffer=6,amethyst=6}
 for id,v in pairs(cap) do local s=D.symbols[id];if s and s.value>v then s.value=v end end
 -- 宝箱开箱奖励同步压平（140 → 60 档）
 local chest={lockbox=10,safe=18,chest=30,mega=60}
 for id,v in pairs(chest) do local s=D.symbols[id];if s and (s.tags.chest or 0)>v then s.tags.chest=v end end
 local d=D.symbols
 if d.colossus then d.colossus.desc=d.colossus.desc:gsub("14金币","8金币") end
 if d.ronin then d.ronin.desc=d.ronin.desc:gsub("独享10金币","独享6金币") end
 if d.puffer then d.puffer.desc=d.puffer.desc:gsub("单张暴赚7币","单张暴赚6币") end
 if d.mega then d.mega.desc=d.mega.desc:gsub("140币","60币");d.mega.desc=d.mega.desc:gsub("350金币","150金币") end
end
-- Keep the seal recipe and its undead product in the same family.
for _,id in ipairs({"banana","peel","thief","billionaire"}) do D.symbols[id].group=3 end
for _,id in ipairs({"banana","peel","thief"}) do D.symbols[id].value=D.symbols[id].value+(B.rules.sealIngredientBonus or 0) end
for _,id in ipairs({"toadstool","puffer","poison_crystal","antidote"}) do D.symbols[id].tags.poison=true end
D.symbols.hydra.desc="场上至少有"..B.rules.hydraMin.."个毒素材料（包括九头蛇）时，每个毒素材料使所有毒素材料的基础产出+"..B.rules.hydraPerPoison.."，并使它们的收益×"..B.rules.hydraMultiplier.."；不足时九头蛇基础产出变为2。"
D.symbols.dream_essence.desc="由相邻【封印符】和【潜行梦魇】在任意炉温下炼成；基础产出"..D.symbols.dream_essence.value.."金币，每瓶让场上使魔与亡灵各增加"..B.rules.dreamEssenceBoost.."基础产出；成功熬制后看破【幻影护盾】。"
D.symbols.magic_key.desc="每次炼制打开一个相邻宝箱，开箱金币×"..B.rules.magicKey.."；魔钥不会消耗。"
-- The final base values include rarity bonuses and balance caps; descriptions
-- must be generated after those changes, not copied from the original cards.
local fruitGain=B.rules.fruitGain or {4,4,4}
D.symbols.fruitking.desc="每次上场向牌袋添加1颗南瓜种子；吞噬相邻水果，普通/罕见/稀有及传奇分别永久成长+"..fruitGain[1].."/"..fruitGain[2].."/"..fruitGain[3].."币（最多+"..B.rules.growCap.fruitking.."），并获得吞噬金币。同场其他水果基础产出+5，自己每有1颗水果基础产出+3。"
D.symbols.seed.desc="上场有"..math.floor(B.rules.seedChance*100+0.5).."%几率随机长成水果或曼陀罗花；位于最底排或南瓜果王在场时必定发芽。"
D.symbols.witch.desc="让相邻使魔类材料收益×"..B.rules.witch.."，包括黑猫、守夜鸮、厄运乌鸦、怨灵、毒苹果和九命灵猫等。"
D.symbols.apple.desc="水果；基础产出"..D.symbols.apple.value.."币，能被相邻暗夜魔女加成；被南瓜果王吞噬时使其永久成长+"..fruitGain[3].."币。"
D.symbols.orange.desc="水果；基础产出"..D.symbols.orange.value.."币，可被南瓜果王吞噬。"
D.symbols.peach.desc="水果；基础产出"..D.symbols.peach.value.."币；被吃掉或销毁时，向牌袋添加1颗南瓜种子。"
D.symbols.coconut_half.desc="水果；基础产出"..D.symbols.coconut_half.value.."币，可被南瓜果王或沼泽巨魔吞噬。"
D.symbols.sapphire.desc="宝石；基础产出"..D.symbols.sapphire.value.."币，可被科学怪人吞噬。"
D.symbols.antidote.desc="基础产出"..D.symbols.antidote.value.."币；留在锅里熬制时，炸锅风险降低"..math.abs(B.brew.materials.antidote.risk).."%。"
D.symbols.hex.desc="基础产出"..D.symbols.hex.value.."币；在场时高品质材料抽取权重÷"..B.rules.hexTedium.."；能被相邻暗夜魔女加成。"
D.symbols.dream_potion.desc="【主线秘药】文火（炉温 0～1）由相邻曼陀罗花与幽灵牛奶炼成；基础产出"..D.symbols.dream_potion.value.."币，成功熬制后破除【恐惧护盾】。"
D.symbols.blood_essence.desc="文火（炉温 0～1）由相邻血族女王与月露圣杯炼成；基础产出"..D.symbols.blood_essence.value.."币。"
D.symbols.purified_ash.desc="由贪念残魂净化而成；基础产出"..D.symbols.purified_ash.value.."币。"
D.symbols.bee.desc="相邻曼陀罗花产出×"..B.rules.bee.."；每有一朵曼陀罗花相邻，自身基础产出+1。"
D.symbols.rain.desc="相邻曼陀罗花产出×"..B.rules.rain.."；相邻南瓜种子发芽率+"..math.floor(B.rules.seedBoost*100+0.5).."%。"
D.symbols.archmage.desc="凑齐水、火、风、地四种元素魔晶时，所有元素材料和全能大魔导的基础产出+8、收益×2.5；元素不足两种时，全能大魔导基础产出变为1。"
D.symbols.lich_emperor.desc="场上至少有2个骨瓮、封魂棺、怨灵、死灵术士或掘墓人时，墓园亡灵流与死灵材料收益×2；不足时冥魂大巫妖基础产出变为2。"
D.symbols.nine_lives_cat.desc="与黑猫、暗夜魔女、守夜鸮或另一只九命灵猫同场时，使魔类材料收益×2；否则自身基础产出变为2。牌袋中有九命灵猫时，每次炸锅消耗1条命并免除惩罚（每只最多9次）。"
D.symbols.ronin.desc="基础产出"..D.symbols.ronin.value.."币；周围有其他人类或使魔时，自身基础产出变为2。"
D.symbols.colossus.desc="基础产出"..D.symbols.colossus.value.."币；使相邻材料的基础产出-1（最低为0）。"
D.symbols.astrologer.desc="基础产出"..D.symbols.astrologer.value.."币；在场时高品质材料抽取权重×1.5；每次上场有50%几率获得1次免费重抽。"
D.symbols.berserker.desc="基础产出"..D.symbols.berserker.value.."币；持有金币少于当夜房租的50%时，自身基础产出+12。"
D.symbols.dame.desc="相邻宝石依次获得血族女王的递减倍率（最多"..B.rules.dameStack.."位，×"..B.rules.dame.."/×"..B.rules.dame2.."/×"..B.rules.dame3.."）；文火（炉温 0～1）时将相邻月露圣杯炼成血月精华，并额外获得"..B.rules.martini.."金币。场上没有宝石或月露圣杯时，自身基础产出-3。"
D.symbols.martini.desc="文火（炉温 0～1）时与相邻血族女王炼成血月精华，并额外获得"..B.rules.martini.."金币。"
for _,id in ipairs({"lockbox","safe","chest","mega"}) do
 local s=D.symbols[id]
 local base=s.tags.chest
 s.desc="被销毁时获得"..base.."金币；被万能魔钥打开时获得"..(base*B.rules.magicKey).."金币。"
 if id~="lockbox" then s.desc=s.desc.."结算时场上没有钥匙，本轮基础产出为1。" end
end
-- 卡面文字统一写基础值；实际收益由 Game:CoinMul() 按夜数放大（HUD 显示「物价×N」）。
return D
