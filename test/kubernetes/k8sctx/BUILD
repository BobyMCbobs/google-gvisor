load("//tools:defs.bzl", "go_library")

package(
    default_applicable_licenses = ["//:license"],
    licenses = ["notice"],
)

go_library(
    name = "k8sctx",
    srcs = [
        "k8sctx.go",
        "k8sctx_impl.go",
    ],
    visibility = [
        "//visibility:public",
    ],
    deps = [
        "//runsc/flag",
        "//test/kubernetes:test_range_config_go_proto",
        "//test/kubernetes/testcluster",
        "//tools/gvisor_k8s_tool/provider/kubectl",
        "@org_golang_google_protobuf//encoding/prototext:go_default_library",
        "@org_golang_google_protobuf//types/known/anypb:go_default_library",
    ],
)
