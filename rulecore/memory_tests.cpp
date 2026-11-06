#include "rules.hpp"
#include <array>
#include <iostream>
int main(){const std::array<std::uint32_t,6> actions{2,2,2,2,2,2};for(int i=0;i<200000;i++){const auto out=miniverse::replay("card-v1",123,actions);if(!out.valid||out.score!=36||out.hash!=8660768014306689972ULL)return 1;const std::array<std::uint32_t,1> invalid{99};if(miniverse::replay("card-v1",123,invalid).valid)return 2;}std::cout<<"200000 valid + 200000 invalid rule replays passed\n";}
