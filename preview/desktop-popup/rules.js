/* Generated copy of team/growth-rules.v1.json. Verified by tests/growth.test.cjs. */
(function(root){ const rules = {
  "version": "growth-v1-candidate",
  "source": "synthetic",
  "dayBoundary": "UTC",
  "maxRewardedDuelsPerDay": 3,
  "rewards": {"card_created": 20, "project_completed": 40, "duel": {"win": 30, "draw": 15, "loss": 10}},
  "ranks": [
    {"id":"seeker","nameZh":"寻光者","nameEn":"Seeker","minXp":0},
    {"id":"torchbearer","nameZh":"执火者","nameEn":"Torchbearer","minXp":60},
    {"id":"shaper","nameZh":"塑形师","nameEn":"Shaper","minXp":200},
    {"id":"maker","nameZh":"造物师","nameEn":"Maker","minXp":600},
    {"id":"worldweaver","nameZh":"织界者","nameEn":"Worldweaver","minXp":1500},
    {"id":"worldbuilder","nameZh":"创世者","nameEn":"Worldbuilder","minXp":3500}
  ]
}; if(typeof module === "object" && module.exports) module.exports=rules; if(root)root.CommunityGrowthRules=rules; })(typeof globalThis === "undefined" ? this : globalThis);