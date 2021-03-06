context("vec-utils")

test_that("vector is modified", {
  x <- c(1, b = 2, c = 3, 4)
  out <- modify(x, 5, b = 20, splice(list(6, c = "30")))
  expect_equal(out, list(1, b = 20, c = "30", 4, 5, 6))
})

test_that("seq2() creates increasing sequences", {
  expect_identical(seq2(2, 3), 2:3)
  expect_identical(seq2(3, 2), int())
})

test_that("seq2_along() creates increasing sequences", {
  expect_identical(seq2_along(3, 1:2), int())
  expect_identical(seq2_along(-1, 1:2), -1:2)
})

test_that("seq2() fails with non-scalar inputs", {
  expect_error(seq2(int(), 1), "must be length one")
  expect_error(seq2(1, int()), "must be length one")
})
