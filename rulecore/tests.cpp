#include "rules.hpp"
#include <iostream>
#include <vector>
int main(){
 const std::vector<std::uint32_t> actions{2,2,2,2,2,2};
 const auto a=miniverse::replay("card-v1",123,actions);
 if(!a.valid||a.score!=36||a.hash!=8660768014306689972ULL||!a.victory||!a.finished){std::cerr<<a.error;return 1;}
 for(int i=0;i<1000;i++){const auto b=miniverse::replay("card-v1",123,actions);if(b.hash!=a.hash||b.score!=a.score)return 2;}
 const std::vector<std::uint32_t> invalid{9};if(miniverse::replay("card-v1",123,invalid).valid)return 3;
 if(miniverse::replay("wrong",123,actions).valid||miniverse::replay("card-v1",0,actions).valid)return 4;
 auto extra=actions;extra.push_back(2);if(miniverse::replay("card-v1",123,extra).valid)return 5;
 std::cout<<"golden score="<<a.score<<" hash="<<a.hash<<" victory="<<a.victory<<" finished="<<a.finished<<"\n";
 return 0;
}
