{

  description = "locus-go — Go reimplementation of the Locus artifact repository proxy";

  inputs = {
    nix-pins.url = "git+ssh://git@git.accur8.net/a8/nix-pins";
  };

  outputs =
    { self, nix-pins, ... }:
    {
      devShells = nix-pins.lib.composeDevShells {
        default =
          { pkgs, frags, ... }:
          let
            # frags.go ships both gopls and gotools, which each provide a
            # `modernize` binary; lower gopls priority to resolve the buildEnv
            # collision (same workaround as the godev flake).
            goFrag = frags.go { };
            goFragFixed = goFrag // {
              packages = map (
                p: if (p.pname or "") == "gopls" then pkgs.lib.lowPrio p else p
              ) goFrag.packages;
            };
          in
          [
            goFragFixed
            (frags.aiCodingAssistants { })
          ];
      };
    };
}
