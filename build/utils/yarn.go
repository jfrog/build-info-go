package utils

import (
	"sync"

	"github.com/jfrog/build-info-go/entities"
	"github.com/jfrog/gofrog/parallel"
)

// Executes traverseDependenciesFunc on all dependencies in dependenciesMap. Each dependency that gets true in return, is added to dependenciesList.
func TraverseDependencies(dependenciesMap map[string]*entities.Dependency, traverseDependenciesFunc func(dependency *entities.Dependency) (bool, error), threads int) (dependenciesList []entities.Dependency, err error) {
	producerConsumer := parallel.NewBounedRunner(threads, false)
	dependenciesChan := make(chan *entities.Dependency)
	errorChan := make(chan error, 1)

	go func() {
		defer producerConsumer.Done()
		for _, dep := range dependenciesMap {
			handlerFunc := createHandlerFunc(dep, dependenciesChan, traverseDependenciesFunc)
			_, err = producerConsumer.AddTaskWithError(handlerFunc, func(err error) {
				// Write the error to the channel, but don't wait if the channel buffer is full.
				select {
				case errorChan <- err:
				default:
					return
				}
			})
			if err != nil {
				return
			}
		}
	}()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		for dep := range dependenciesChan {
			dependenciesList = append(dependenciesList, *dep)
		}
		wg.Done()
	}()

	producerConsumer.Run()
	if err != nil {
		return
	}
	close(dependenciesChan)
	wg.Wait()

	// Read the error from the channel, but don't wait if the channel buffer is empty.
	select {
	case err = <-errorChan:
	default:
		return
	}
	return
}

// createHandlerFunc creates a function that runs traverseDependenciesFunc (if it's not nil) with dep as its parameter.
// If traverseDependenciesFunc returns false, then dep will not be saved in the module's dependencies list.
func createHandlerFunc(dep *entities.Dependency, dependenciesChan chan *entities.Dependency, traverseDependenciesFunc func(dependency *entities.Dependency) (bool, error)) func(threadId int) error {
	return func(threadId int) error {
		var err error
		saveDep := true
		if traverseDependenciesFunc != nil {
			saveDep, err = traverseDependenciesFunc(dep)
			if err != nil {
				return err
			}
		}
		if saveDep {
			dependenciesChan <- dep
		}
		return nil
	}
}
