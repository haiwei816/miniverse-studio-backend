#pragma once
#include <cstdint>
#include <span>
#include <string>
namespace miniverse {
struct Result { bool valid=true; std::int64_t score=0; std::uint64_t hash=14695981039346656037ULL; std::string error; bool finished=false; bool victory=false; };
inline std::uint32_t next(std::uint32_t& state){state^=state<<13;state^=state>>17;state^=state<<5;return state;}
inline Result replay(const std::string& version,std::uint32_t seed,std::span<const std::uint32_t> actions){
 Result r;
 auto fail=[&](const char* error){r.valid=false;r.error=error;r.score=0;return r;};
 if(version!="card-v1")return fail("unsupported_version");
 if(seed==0||actions.empty()||actions.size()>128)return fail("invalid_replay_bounds");
 std::int32_t hp=20,enemy=30;std::uint32_t rng=seed;std::int64_t damage=0;
 auto hash32=[&](std::uint32_t v){for(int i=0;i<4;i++){r.hash^=(v>>(i*8))&255U;r.hash*=1099511628211ULL;}};
 hash32(seed);
 for(auto action:actions){
  if(r.finished)return fail("action_after_terminal");
  if(action>2)return fail("illegal_action");
  const auto dealt=action==0?0:static_cast<std::int32_t>(next(rng)%6+action*2);
  const auto actual=dealt<enemy?dealt:enemy;enemy-=actual;damage+=actual;
  if(enemy>0){const auto attack=static_cast<std::int32_t>(next(rng)%4+1);hp-=action==0?attack/2:attack;}
  r.finished=enemy<=0||hp<=0;r.victory=enemy<=0&&hp>0;
  hash32(action);hash32(static_cast<std::uint32_t>(hp));hash32(static_cast<std::uint32_t>(enemy));hash32(rng);
 }
 r.score=damage+(r.victory?hp:0);return r;
}
}
